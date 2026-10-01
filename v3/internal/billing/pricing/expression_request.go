package pricing

import (
	"fmt"
	"math/big"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/tidwall/gjson"
)

func (e *expressionEnvironment) requestFunction(name string, args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("pricing: %s requires one argument", name)
	}
	path, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("pricing: %s argument must be string", name)
	}
	switch name {
	case "header":
		return e.requestHeader(path), nil
	case "param":
		return e.requestParam(path)
	}
	return e.requestTimeField(name, path)
}

func (e *expressionEnvironment) requestHeader(path string) string {
	for key, value := range e.request.Headers {
		if strings.EqualFold(strings.TrimSpace(key), strings.TrimSpace(path)) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (e *expressionEnvironment) requestParam(path string) (any, error) {
	value := gjson.GetBytes(e.request.Body, strings.TrimSpace(path))
	if !value.Exists() || value.Type == gjson.Null {
		return nil, nil
	}
	switch value.Type {
	case gjson.String:
		return value.Str, nil
	case gjson.True:
		return true, nil
	case gjson.False:
		return false, nil
	case gjson.Number:
		return catalogNumber(value.Raw)
	default:
		return nil, fmt.Errorf("pricing: param %q must be a scalar", path)
	}
}

// requestTimeField evaluates hour/minute/weekday/month/day against the
// frozen request time, interpreted in the timezone named by path (UTC if
// empty).
func (e *expressionEnvironment) requestTimeField(name, path string) (any, error) {
	if e.request.Now.IsZero() {
		return nil, fmt.Errorf("pricing: time expression requires frozen request time")
	}
	location := time.UTC
	if strings.TrimSpace(path) != "" {
		var err error
		location, err = time.LoadLocation(path)
		if err != nil {
			return nil, fmt.Errorf("pricing: invalid timezone %q: %w", path, err)
		}
	}
	now := e.request.Now.In(location)
	var value int
	switch name {
	case "hour":
		value = now.Hour()
	case "minute":
		value = now.Minute()
	case "weekday":
		value = int(now.Weekday())
	case "month":
		value = int(now.Month())
	case "day":
		value = now.Day()
	}
	return new(big.Rat).SetInt64(int64(value)), nil
}
