package legacy

import (
	"encoding/json"
	"strings"
)

func commercePaymentIdentity(name string, row commerceRow, values map[string]any) error {
	values["provider_reference"], values["payment_event_id"] = nil, nil
	external, err := row.text("external_payment_id")
	if err != nil {
		return err
	}
	external = strings.TrimSpace(external)
	if name == "top_ups" && external != "" {
		values["provider_reference"] = external
		values["payment_event_id"] = external
	}
	if values["provider"] == "epay" || values["provider"] == "xunhu" {
		values["provider_reference"] = values["trade_no"]
	}
	if values["provider"] != "epay" || values["payment_event_id"] != nil {
		return nil
	}
	// Epay's original verified subscription response serializes TradeNo;
	// original topup callbacks retain trade_no. Neither is the merchant ID.
	// Unparseable/missing historical evidence remains explicitly unrefundable.
	var payload map[string]json.RawMessage
	if json.Unmarshal([]byte(values["provider_payload"].(string)), &payload) != nil {
		return nil
	}
	for _, key := range []string{"trade_no", "TradeNo"} {
		var transaction string
		if json.Unmarshal(payload[key], &transaction) == nil && strings.TrimSpace(transaction) != "" {
			values["payment_event_id"] = strings.TrimSpace(transaction)
			break
		}
	}
	return nil
}
