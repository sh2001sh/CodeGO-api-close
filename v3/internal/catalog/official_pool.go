package catalog

import (
	"encoding/json"
	"math/big"
	"strings"
)

// OfficialPool preserves the group-scoped scored routing contract. Empty
// ModelScope is wildcard; a literal "*" remains an exact model name.
type OfficialPool struct {
	ID               int64                `json:"id"`
	Name             string               `json:"name"`
	Group            string               `json:"group"`
	ModelScope       string               `json:"model_scope"`
	AutoDiscover     bool                 `json:"auto_discover"`
	MultiplierWeight int                  `json:"multiplier_weight"`
	TTFTWeight       int                  `json:"ttft_weight"`
	CacheWeight      int                  `json:"cache_weight"`
	SuccessWeight    int                  `json:"success_weight"`
	Members          []OfficialPoolMember `json:"members"`
}

type OfficialPoolMember struct {
	ChannelID          int64                  `json:"channel_id"`
	CostMultiplier     string                 `json:"cost_multiplier"`
	ModelCostOverrides map[string]json.Number `json:"model_cost_overrides"`
	FaultDomain        string                 `json:"fault_domain"`
	Models             []string               `json:"models"`
}

func (p OfficialPool) Matches(model string) bool {
	scope := strings.TrimSpace(p.ModelScope)
	return scope == "" || strings.EqualFold(scope, strings.TrimSpace(model))
}

func (m OfficialPoolMember) Cost(model string) string {
	if value, ok := m.ModelCostOverrides[model]; ok {
		return string(value)
	}
	return m.CostMultiplier
}

// PositivePoolDecimal validates JSON decimal syntax without rounding through
// float64. Its size limit bounds untrusted control API numeric parsing.
func PositivePoolDecimal(value string) bool {
	if len(value) == 0 || len(value) > 256 || !json.Valid([]byte(value)) || value[0] == '"' {
		return false
	}
	if at := strings.IndexAny(value, "eE"); at >= 0 {
		exp, ok := new(big.Int).SetString(value[at+1:], 10)
		if !ok || !exp.IsInt64() || exp.Int64() > 1000 || exp.Int64() < -1000 {
			return false
		}
	}
	number, ok := new(big.Rat).SetString(value)
	return ok && number.Sign() > 0
}
