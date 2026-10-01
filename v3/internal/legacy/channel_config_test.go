package legacy

import (
	"encoding/json"
	"testing"
)

func TestMigratedProviderMetadataAndSeparateCardSwitch(t *testing.T) {
	for _, kind := range []int{39, 49} {
		row, _ := json.Marshal(map[string]any{"id": 1, "type": kind, "name": "native", "key": "retained-token", "other": "retained-id", "base_url": "https://example.test", "multiplier_card_supported": true, "multiplier_card_user_enabled": false, "setting": `{"proxy":"socks5://127.0.0.1:1080","force_format":true}`, "settings": `{"allow_service_tier":true}`})
		c, _, err := decodeChannel(row)
		if err != nil {
			t.Fatal(err)
		}
		var secret map[string]string
		if json.Unmarshal([]byte(c.Key), &secret) != nil {
			t.Fatal("token and account/bot not combined")
		}
		field := "account_id"
		if kind == 49 {
			field = "bot_id"
		}
		if secret[field] != "retained-id" || secret["token"] != "retained-token" {
			t.Fatal("provider metadata lost")
		}
		var settings map[string]any
		if err = json.Unmarshal([]byte(c.Settings), &settings); err != nil {
			t.Fatal(err)
		}
		if settings["multiplier_card_user_enabled"] != false || settings["force_format"] != true || c.ProxyURL != "socks5://127.0.0.1:1080" {
			t.Fatal("network or card switches lost")
		}
	}
	for _, kind := range []int{3, 41} {
		row, _ := json.Marshal(map[string]any{"id": 1, "type": kind, "name": "native", "key": `{"project_id":"project","private_key":"dummy"}`, "other": `{"default":"global","client-model":"us-east5"}`, "base_url": "https://example.test"})
		c, _, err := decodeChannel(row)
		if err != nil {
			t.Fatal(err)
		}
		var settings map[string]any
		_ = json.Unmarshal([]byte(c.Settings), &settings)
		if settings["api_version"] != `{"default":"global","client-model":"us-east5"}` || c.Key != `{"project_id":"project","private_key":"dummy"}` {
			t.Fatal("region map or service account changed")
		}
	}
}

func TestMigratedChannelRejectsBadNetworkSettings(t *testing.T) {
	for _, setting := range []string{`[]`, `{"proxy":2}`, `{"proxy":"file:///tmp/test"}`} {
		row, _ := json.Marshal(map[string]any{"id": 1, "type": 1, "name": "native", "key": "dummy", "base_url": "https://example.test", "setting": setting})
		if _, _, err := decodeChannel(row); err == nil {
			t.Fatal("invalid settings accepted")
		}
	}
}
