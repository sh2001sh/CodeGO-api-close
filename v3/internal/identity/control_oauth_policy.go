package identity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"slices"
	"strings"

	"github.com/tidwall/gjson"
)

type oauthAccessPolicy struct {
	Logic      string              `json:"logic"`
	Conditions []oauthCondition    `json:"conditions"`
	Groups     []oauthAccessPolicy `json:"groups"`
}

type oauthCondition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

type oauthPolicyDenial struct {
	condition oauthCondition
	current   any
	message   string
}

func (e *oauthPolicyDenial) Error() string { return "identity: OAuth access policy denied" }
func (e *oauthPolicyDenial) Unwrap() error { return ErrForbidden }

func checkOAuthPolicy(body []byte, raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	if len(raw) > 65536 {
		return ErrInvalidInput
	}
	var policy oauthAccessPolicy
	d := json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	d.DisallowUnknownFields()
	if d.Decode(&policy) != nil || !errors.Is(d.Decode(new(any)), io.EOF) {
		return ErrInvalidInput
	}
	count := 0
	if err := validateOAuthPolicy(&policy, 0, &count); err != nil {
		return err
	}
	allowed, denied := evaluateOAuthPolicy(body, policy)
	if !allowed {
		return denied
	}
	return nil
}

func validateOAuthPolicy(p *oauthAccessPolicy, depth int, count *int) error {
	if depth > 16 || len(p.Conditions)+len(p.Groups) == 0 {
		return ErrInvalidInput
	}
	p.Logic = strings.ToLower(strings.TrimSpace(p.Logic))
	if p.Logic == "" {
		p.Logic = "and"
	}
	if p.Logic != "and" && p.Logic != "or" {
		return ErrInvalidInput
	}
	for i := range p.Conditions {
		*count++
		if *count > 512 {
			return ErrInvalidInput
		}
		c := &p.Conditions[i]
		c.Field = strings.TrimSpace(c.Field)
		c.Op = strings.ToLower(strings.TrimSpace(c.Op))
		if c.Field == "" || len(c.Field) > 128 || !slices.Contains([]string{"eq", "ne", "gt", "gte", "lt", "lte", "in", "not_in", "contains", "not_contains", "exists", "not_exists"}, c.Op) {
			return ErrInvalidInput
		}
		if c.Op == "in" || c.Op == "not_in" {
			if _, ok := c.Value.([]any); !ok {
				return ErrInvalidInput
			}
		}
	}
	for i := range p.Groups {
		if err := validateOAuthPolicy(&p.Groups[i], depth+1, count); err != nil {
			return err
		}
	}
	return nil
}

func evaluateOAuthPolicy(body []byte, p oauthAccessPolicy) (bool, *oauthPolicyDenial) {
	var first *oauthPolicyDenial
	results := make([]bool, 0, len(p.Conditions)+len(p.Groups))
	for _, c := range p.Conditions {
		result := gjson.GetBytes(body, c.Field)
		var current any
		if result.Exists() {
			d := json.NewDecoder(bytes.NewBufferString(result.Raw))
			d.UseNumber()
			_ = d.Decode(&current)
		}
		ok := oauthCompareCondition(result.Exists(), current, c)
		results = append(results, ok)
		if !ok && first == nil {
			first = &oauthPolicyDenial{condition: c, current: current}
		}
	}
	for _, group := range p.Groups {
		ok, denied := evaluateOAuthPolicy(body, group)
		results = append(results, ok)
		if !ok && first == nil {
			first = denied
		}
	}
	for _, ok := range results {
		if p.Logic == "or" && ok {
			return true, nil
		}
		if p.Logic == "and" && !ok {
			return false, first
		}
	}
	if p.Logic == "or" {
		return false, first
	}
	return true, nil
}

func oauthCompareCondition(exists bool, current any, c oauthCondition) bool {
	// Ordered comparisons cannot turn an absent or nonnumeric claim into a
	// higher trust level through lexical ordering (for example "<nil>" > "1").
	if c.Op == "gt" || c.Op == "gte" || c.Op == "lt" || c.Op == "lte" {
		if !exists || current == nil || c.Value == nil {
			return false
		}
		_, numericExpected := new(big.Rat).SetString(strings.TrimSpace(fmt.Sprint(c.Value)))
		_, numericCurrent := new(big.Rat).SetString(strings.TrimSpace(fmt.Sprint(current)))
		if numericExpected != numericCurrent {
			return false
		}
	}
	comparison := oauthCompare(current, c.Value)
	switch c.Op {
	case "exists":
		return exists
	case "not_exists":
		return !exists
	case "eq":
		return comparison == 0
	case "ne":
		return comparison != 0
	case "gt":
		return comparison > 0
	case "gte":
		return comparison >= 0
	case "lt":
		return comparison < 0
	case "lte":
		return comparison <= 0
	case "in", "not_in":
		found := slices.ContainsFunc(c.Value.([]any), func(value any) bool { return oauthCompare(current, value) == 0 })
		if c.Op == "not_in" {
			return !found
		}
		return found
	case "contains", "not_contains":
		found := false
		switch value := current.(type) {
		case string:
			found = strings.Contains(value, strings.TrimSpace(fmt.Sprint(c.Value)))
		case []any:
			found = slices.ContainsFunc(value, func(value any) bool { return oauthCompare(value, c.Value) == 0 })
		}
		if c.Op == "not_contains" {
			return !found
		}
		return found
	}
	return false
}

func oauthCompare(left, right any) int {
	l, r := strings.TrimSpace(fmt.Sprint(left)), strings.TrimSpace(fmt.Sprint(right))
	ln, lok := new(big.Rat).SetString(l)
	rn, rok := new(big.Rat).SetString(r)
	if lok && rok {
		return ln.Cmp(rn)
	}
	return strings.Compare(l, r)
}

func renderOAuthDenial(template, provider string, body []byte, e *oauthPolicyDenial) string {
	message := strings.TrimSpace(template)
	if message == "" {
		return "Access denied: your account does not meet this provider's access requirements."
	}
	for token, value := range map[string]string{"{{provider}}": provider, "{{field}}": e.condition.Field, "{{op}}": e.condition.Op, "{{required}}": fmt.Sprint(e.condition.Value), "{{current}}": fmt.Sprint(e.current)} {
		message = strings.ReplaceAll(message, token, value)
	}
	pattern := regexp.MustCompile(`\{\{(current|required)\.([^}]+)\}\}`)
	message = pattern.ReplaceAllStringFunc(message, func(token string) string {
		parts := pattern.FindStringSubmatch(token)
		if parts[1] == "current" {
			return gjson.GetBytes(body, strings.TrimSpace(parts[2])).String()
		}
		if parts[2] == e.condition.Field {
			return fmt.Sprint(e.condition.Value)
		}
		return ""
	})
	return message
}
