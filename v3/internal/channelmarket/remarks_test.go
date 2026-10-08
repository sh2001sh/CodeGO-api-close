package channelmarket

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"
)

func TestPublicRemarksNormalizeProseAndRejectContactAdvertising(t *testing.T) {
	for _, value := range []string{
		"", "兼容 OpenAI 协议，支持流式输出、缓存与工具调用。", "Supports streaming, 128K context and tools.",
		"繁體中文說明；多模態模型。", "Modèles pour la recherche.", "日本語の説明。", "Модели для исследований.", "한국어 모델 설명.", "نماذج للبحث، تدعم الأدوات.", "AI 🤖 研究模型。",
		strings.Repeat("模", 200),
	} {
		if got, err := normalizeGroupRemark(value); err != nil || got != norm.NFKC.String(value) {
			t.Fatalf("valid remark %q -> %q %v", value, got, err)
		}
	}
	if got, err := normalizeGroupRemark("  支持流式输出。\n支持工具调用。  "); err != nil || got != "支持流式输出。 支持工具调用。" {
		t.Fatalf("prose whitespace normalization %q %v", got, err)
	}
	for _, value := range []string{strings.Repeat("模", 201), "联系客服", "加微 abc", "t.me/route", "user@example.com", "电话 13800138000", "電 話 １３８００１３８０００", "١٣٨٠٠١٣٨٠٠٠", "官方认证", "Buy cheap credits", "example.com", "ＧＰＴ ｅｘａｍｐｌｅ．ｃｏｍ", "模型\u202e说明", "模型\u200b说明", "<script>ad</script>"} {
		if _, err := normalizeGroupRemark(value); !errors.Is(err, ErrInvalidRemark) {
			t.Fatalf("accepted prohibited remark %q: %v", value, err)
		}
	}
	request := CreateRequest{Provider: "openai_compatible", Name: "legacy.example.com", Remark: "legacy@example.com", BaseURL: "https://example.com", APIKey: "placeholder", Models: []string{"model"}, Prices: json.RawMessage(`{}`), Multiplier: "1"}
	for _, patch := range []map[string]json.RawMessage{{"max_concurrency": json.RawMessage("10")}, {"name": json.RawMessage(`"legacy.example.com"`), "remark": json.RawMessage(`"legacy@example.com"`)}} {
		if _, _, err := mergeAndValidateUpdate(request, patch); err != nil {
			t.Fatalf("unchanged legacy presentation blocks other edits: %v", err)
		}
	}
	w := httptest.NewRecorder()
	New(nil, nil, nil, Config{}, nil).result(w, nil, ErrInvalidRemark)
	if w.Code != 400 || !strings.Contains(w.Body.String(), `"code":"invalid_group_remark"`) {
		t.Fatalf("remark response %d %s", w.Code, w.Body.String())
	}
}
