package legacy

import (
	"encoding/json"
	"testing"
)

func TestMarketplaceEffectivePoolPreservesSourceLegacyNormalization(t *testing.T) {
	const custom = `[{"name":"custom","min_usd":7,"max_usd":7,"probability":1,"reward_type":"claude_quota"}]`
	for _, tc := range []struct {
		name, balance, want string
	}{
		{"missing balance retains initialized default", "", marketplaceBalanceTiers},
		{"explicit empty balance uses standard", "[]", custom},
		{"old balance uses effective standard", marketplaceLegacyFixtureTiers(t), custom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := map[string]string{"blind_box_setting.tiers": custom}
			if tc.balance != "" {
				options["blind_box_setting.balance_blind_box_tiers"] = tc.balance
			}
			marketplaceEffectivePoolOptions(options)
			if got := options["blind_box_setting.balance_blind_box_tiers"]; got != tc.want {
				t.Fatalf("effective balance=%s want %s", got, tc.want)
			}
		})
	}
}

func marketplaceLegacyFixtureTiers(t *testing.T) string {
	t.Helper()
	probabilities := []string{"0.30", "0.17", "0.10", "0.35", "0.055", "0.01", "0.001", "0.00025", "0.00004", "0.00001", "0.0127", "0.001"}
	tiers := make([]marketplaceTier, len(probabilities))
	for i, p := range probabilities {
		tiers[i] = marketplaceTier{Name: "stale source reward", Min: json.Number("1"), Max: json.Number("1"), Probability: json.Number(p), Kind: "claude_quota"}
	}
	raw, err := json.Marshal(tiers)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestMarketplaceLegacyStandardPrizeFallbackIsNotReactivated(t *testing.T) {
	names := []string{"5 美元普通额度", "8 美元普通额度", "12 美元普通额度", "20 美元 Claude 额度", "30 美元 Claude 额度", "充值九折卡", "套餐九折卡", "0.95 倍率卡", "0.9 倍率卡", "免费调用次数卡（10 次）"}
	amounts := []string{"5", "8", "12", "20", "30", "0", "0", "0", "0", "0"}
	probabilities := []string{"0.10", "0.16", "0.18", "0.20", "0.14", "0.08", "0.07", "0.04", "0.03", "0.02"}
	tiers := make([]marketplaceTier, len(names))
	for i, name := range names {
		tiers[i] = marketplaceTier{Name: name, Min: json.Number(amounts[i]), Max: json.Number(amounts[i]), Probability: json.Number(probabilities[i])}
	}
	raw, err := json.Marshal(tiers)
	if err != nil {
		t.Fatal(err)
	}
	options := map[string]string{"blind_box_setting.tiers": string(raw), "blind_box_setting.balance_blind_box_tiers": "[]", "blind_box_setting.subscription_prize_probability": "0.001", "blind_box_setting.subscription_plan_title": "Standard月卡"}
	marketplaceEffectivePoolOptions(options)
	if options["blind_box_setting.balance_blind_box_tiers"] != marketplaceBalanceTiers || options["blind_box_setting.subscription_prize_probability"] != "0.003" || options["blind_box_setting.subscription_plan_title"] != "Lite月卡" {
		t.Fatalf("stale source options were revived: %+v", options)
	}
}
