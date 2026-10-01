package catalog

import (
	"encoding/json"
	"strings"
)

const (
	NameRuleExact = iota
	NameRulePrefix
	NameRuleContains
	NameRuleSuffix
)

type VendorMetadata struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Status      int    `json:"status"`
	CreatedTime int64  `json:"created_time"`
	UpdatedTime int64  `json:"updated_time"`
}

type ModelMetadata struct {
	ID           int64  `json:"id"`
	ModelName    string `json:"model_name"`
	Description  string `json:"description,omitempty"`
	Icon         string `json:"icon,omitempty"`
	Tags         string `json:"tags,omitempty"`
	Endpoints    string `json:"endpoints,omitempty"`
	VendorID     int64  `json:"vendor_id,omitempty"`
	Status       int    `json:"status"`
	SyncOfficial int    `json:"sync_official"`
	NameRule     int    `json:"name_rule"`
	CreatedTime  int64  `json:"created_time"`
	UpdatedTime  int64  `json:"updated_time"`
}

type PrefillGroup struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	Description string          `json:"description,omitempty"`
	Items       json.RawMessage `json:"items"`
	CreatedTime int64           `json:"created_time"`
	UpdatedTime int64           `json:"updated_time"`
}

type MetadataSnapshot struct {
	Vendors       []VendorMetadata `json:"vendors"`
	Models        []ModelMetadata  `json:"models"`
	PrefillGroups []PrefillGroup   `json:"prefill_groups"`
}

// MatchesModelName retains the source exact/prefix/contains/suffix rules. A
// literal '*' or '%' is ordinary text, never a SQL or routing wildcard.
func MatchesModelName(pattern string, rule int, name string) bool {
	if pattern == "" || name == "" {
		return false
	}
	switch rule {
	case NameRuleExact:
		return name == pattern
	case NameRulePrefix:
		return strings.HasPrefix(name, pattern)
	case NameRuleContains:
		return strings.Contains(name, pattern)
	case NameRuleSuffix:
		return strings.HasSuffix(name, pattern)
	default:
		return false
	}
}

// Describe uses the source pricing precedence, independent of slice ordering.
// Lower retained IDs break ties; metadata status is descriptive, while actual
// availability is determined by channels, credentials and routing permission.
func (m MetadataSnapshot) Describe(name string) (*ModelMetadata, *VendorMetadata) {
	var matched *ModelMetadata
	for i := range m.Models {
		candidate := &m.Models[i]
		if !MatchesModelName(candidate.ModelName, candidate.NameRule, name) {
			continue
		}
		if matched == nil || metadataRank(candidate.NameRule) < metadataRank(matched.NameRule) ||
			(candidate.NameRule == matched.NameRule && candidate.ID < matched.ID) {
			matched = candidate
		}
	}
	if matched != nil && matched.VendorID != 0 {
		for i := range m.Vendors {
			if m.Vendors[i].ID == matched.VendorID {
				return matched, &m.Vendors[i]
			}
		}
	}
	return matched, nil
}

func metadataRank(rule int) int {
	switch rule {
	case NameRuleExact:
		return 0
	case NameRulePrefix:
		return 1
	case NameRuleSuffix:
		return 2
	default:
		return 3
	}
}
