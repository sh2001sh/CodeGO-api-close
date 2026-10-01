package legacy

func marketplaceNamedProp(title string) (map[string]any, bool) {
	reward := map[string]any{"kind": "multiplier", "title": title, "weight": int64(1)}
	switch title {
	case "再来一抽":
		reward["kind"] = "extra_draw"
	case "九折充值卡", "充值九折卡":
		reward["kind"] = "topup_discount"
		reward["prop_type"] = "topup_discount_90"
		reward["discount_rate_ppm"] = int64(100000)
	case "套餐九折卡":
		reward["kind"] = "subscription_discount"
		reward["prop_type"] = "subscription_discount_90"
		reward["discount_rate_ppm"] = int64(100000)
	case "0.95 倍率卡":
		reward["multiplier_ppm"] = int64(950000)
		reward["duration_seconds"] = int64(86400)
		reward["prop_type"] = "consume_discount_95"
	case "0.9 倍率卡":
		reward["multiplier_ppm"] = int64(900000)
		reward["duration_seconds"] = int64(86400)
		reward["prop_type"] = "consume_discount_90"
	case "15 分钟 0.1 倍率卡", "0.10 倍率体验卡", "0.1 倍率卡":
		reward["multiplier_ppm"] = int64(100000)
		reward["duration_seconds"] = int64(900)
		reward["prop_type"] = "consume_discount_10"
	case "1 小时 0 倍率卡":
		reward["multiplier_ppm"] = int64(0)
		reward["duration_seconds"] = int64(3600)
		reward["prop_type"] = "zero_hour_multiplier"
	default:
		return nil, false
	}
	return reward, true
}
