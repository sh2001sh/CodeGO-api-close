package channelmarket

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGroupNamesRetainTechnicalAndMultilingualLabels(t *testing.T) {
	for _, value := range []string{"通用模型", "繁體中文", "GPT-4.1", "Qwen3-235B-A22B-Instruct-2507", "研究チャンネル", "한국어 모델", "Модели", "Modèles", "Forschung", "نماذج اللغة", strings.Repeat("模", 40)} {
		got, err := normalizeGroupName(value)
		if err != nil || got != value {
			t.Fatalf("name %q -> %q, %v", value, got, err)
		}
	}
	if got, err := normalizeGroupName("  ＧＰＴ－４.１   香港 "); err != nil || got != "GPT-4.1 香港" {
		t.Fatalf("normalization %q %v", got, err)
	}
}

func TestGroupNamesRejectAdvertisingAndContactObfuscation(t *testing.T) {
	for _, value := range []string{
		"A", strings.Repeat("模", 41), "广告渠道", "廣告渠道", "加 微 信 abc", "低 價充值", "官方 OpenAI", "Code Go AI", "O f f i c i a l",
		"https://example.com", "example.com", "t.me/codego", "user@example.com", "ＧＰＴ ｅｘａｍｐｌｅ．ｃｏｍ",
		"+852 1234 5678", "QQ 12345", "１３８００１３８０００", "١٣٨٠٠١٣٨٠٠٠", "GPT\u200b渠道", "GPT\u202e渠道", "GPT\n渠道", "GPT<script>", "---", "\u0301",
	} {
		if _, err := normalizeGroupName(value); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("accepted prohibited name %q: %v", value, err)
		}
	}
}

func TestNameValidationEnforcedOnCreateAndExplicitRename(t *testing.T) {
	s := New(nil, nil, nil, Config{}, nil)
	if _, err := s.Create(context.Background(), 1, CreateRequest{Name: "微信 123"}); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("create bypassed name validation: %v", err)
	}
	r := CreateRequest{Name: "legacy.example.com", Provider: "openai_compatible", BaseURL: "https://example.com", APIKey: "placeholder", Models: []string{"model"}, Prices: json.RawMessage(`{}`), Multiplier: "1"}
	if _, _, err := mergeAndValidateUpdate(r, map[string]json.RawMessage{"max_concurrency": json.RawMessage("10")}); err != nil {
		t.Fatalf("unrelated edit of legacy label rejected: %v", err)
	}
	for _, name := range []string{"example.com", "微信 123"} {
		encoded, _ := json.Marshal(name)
		if _, _, err := mergeAndValidateUpdate(r, map[string]json.RawMessage{"name": encoded}); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("rename bypassed policy for %q: %v", name, err)
		}
	}
	if updated, _, err := mergeAndValidateUpdate(r, map[string]json.RawMessage{"name": json.RawMessage(`""`)}); err != nil || updated.Name != "openai_compatible" {
		t.Fatalf("empty rename did not select safe system default: %+v %v", updated, err)
	}
	if err := validateUpdateFields(Actor{UserID: 1}, map[string]json.RawMessage{"group_id": json.RawMessage(`"replacement"`)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("mutable stable id: %v", err)
	}
	w := httptest.NewRecorder()
	s.result(w, nil, ErrInvalidName)
	if w.Code != 400 || !strings.Contains(w.Body.String(), `"code":"invalid_group_name"`) {
		t.Fatalf("name error response %d %s", w.Code, w.Body.String())
	}
}
