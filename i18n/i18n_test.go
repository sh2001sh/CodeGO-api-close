package i18n

import (
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"

	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
)

func TestAllInterfaceLanguagesHaveCompleteMessages(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	read := func(lang string) map[string]string {
		t.Helper()
		body, err := localeFS.ReadFile("locales/" + lang + ".yaml")
		if err != nil {
			t.Fatal(err)
		}
		var messages map[string]string
		if err := yaml.Unmarshal(body, &messages); err != nil {
			t.Fatal(err)
		}
		return messages
	}
	pattern := regexp.MustCompile(`\{\{[^{}]+\}\}`)
	parameters := func(value string) string {
		matches := pattern.FindAllString(value, -1)
		sort.Strings(matches)
		return strings.Join(matches, "|")
	}
	english := read(LangEn)
	for _, lang := range SupportedLanguages() {
		messages := read(lang)
		if len(messages) != len(english) {
			t.Errorf("%s has %d messages, want %d", lang, len(messages), len(english))
		}
		for key, source := range english {
			value := messages[key]
			if strings.TrimSpace(value) == "" || parameters(value) != parameters(source) {
				t.Errorf("%s: incomplete translation or changed parameters for %s", lang, key)
			}
		}
		for _, maximum := range []int{0, 100} {
			value := Translate(lang, "common.batch_too_many", map[string]any{"Max": maximum})
			if value == "common.batch_too_many" || strings.Contains(value, "{{") {
				t.Errorf("%s: interpolation failed: %s", lang, value)
			}
		}
		if lang != LangEn && Translate(lang, "common.invalid_params") == english["common.invalid_params"] {
			t.Errorf("%s fell back to English", lang)
		}
	}
}

func TestRequestLanguageOverridesSavedPreferenceWithoutChangingIt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Set(string(constant.ContextKeyUserSetting), dto.UserSetting{Language: LangZhCN})
	for input, expected := range map[string]string{
		"zh-HK": LangZhTW, "zh_Hant": LangZhTW, "zh-CN": LangZhCN,
		"fr-FR, en;q=0.8": LangFr, "ru-RU": LangRu, "ja-JP": LangJa, "vi-VN": LangVi,
		"unsupported": LangEn,
	} {
		c.Request.Header.Set("Accept-Language", input)
		if got := GetLangFromContext(c); got != expected {
			t.Errorf("%q: got %q, want %q", input, got, expected)
		}
	}
	c.Request.Header.Del("Accept-Language")
	if got := GetLangFromContext(c); got != LangZhCN {
		t.Fatalf("stored language changed: %s", got)
	}
	if GetLangFromContext(nil) != DefaultLang {
		t.Fatal("nil context must use the default")
	}
}
