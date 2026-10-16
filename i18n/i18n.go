package i18n

import (
	"embed"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"

	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
)

const (
	LangZhCN    = "zh-CN"
	LangZhTW    = "zh-TW"
	LangEn      = "en"
	LangFr      = "fr"
	LangRu      = "ru"
	LangJa      = "ja"
	LangVi      = "vi"
	DefaultLang = LangEn // Fallback to English if language not supported
)

//go:embed locales/*.yaml
var localeFS embed.FS

var (
	bundle     *i18n.Bundle
	localizers = make(map[string]*i18n.Localizer)
	mu         sync.RWMutex
	initOnce   sync.Once
)

// Init initializes the i18n bundle and loads all translation files
func Init() error {
	var initErr error
	initOnce.Do(func() {
		bundle = i18n.NewBundle(language.Chinese)
		bundle.RegisterUnmarshalFunc("yaml", yaml.Unmarshal)

		// Load embedded translation files
		for _, lang := range SupportedLanguages() {
			_, err := bundle.LoadMessageFileFS(localeFS, "locales/"+lang+".yaml")
			if err != nil {
				initErr = err
				return
			}
		}

		// Pre-create localizers for supported languages
		for _, lang := range SupportedLanguages() {
			localizers[lang] = i18n.NewLocalizer(bundle, lang)
		}

		httpapi.TranslateMessage = T
	})
	return initErr
}

// GetLocalizer returns a localizer for the specified language
func GetLocalizer(lang string) *i18n.Localizer {
	lang = normalizeLang(lang)

	mu.RLock()
	loc, ok := localizers[lang]
	mu.RUnlock()

	if ok {
		return loc
	}

	// Create new localizer for unknown language (fallback to default)
	mu.Lock()
	defer mu.Unlock()

	// Double-check after acquiring write lock
	if loc, ok = localizers[lang]; ok {
		return loc
	}

	loc = i18n.NewLocalizer(bundle, lang, DefaultLang)
	localizers[lang] = loc
	return loc
}

// T translates a message key using the language from gin context
func T(c *gin.Context, key string, args ...map[string]any) string {
	lang := GetLangFromContext(c)
	return Translate(lang, key, args...)
}

// Translate translates a message key for the specified language
func Translate(lang, key string, args ...map[string]any) string {
	loc := GetLocalizer(lang)

	config := &i18n.LocalizeConfig{
		MessageID: key,
	}

	if len(args) > 0 && args[0] != nil {
		config.TemplateData = args[0]
	}

	msg, err := loc.Localize(config)
	if err != nil {
		// Return key as fallback if translation not found
		return key
	}
	return msg
}

// userLangLoaderFunc is a function that loads user language from database/cache
// It's set by the model package to avoid circular imports
var userLangLoaderFunc func(userId int) string

// SetUserLangLoader sets the function to load user language (called from model package)
func SetUserLangLoader(loader func(userId int) string) {
	userLangLoaderFunc = loader
}

// GetLangFromContext extracts the language setting from gin context
// An explicit Accept-Language preference takes priority over the saved account
// language, followed by middleware context and the default language.
func GetLangFromContext(c *gin.Context) string {
	if c == nil {
		return DefaultLang
	}
	// An explicit request preference follows the language currently selected in
	// the interface without writing that selection to the user's account.
	if acceptLang := c.GetHeader("Accept-Language"); acceptLang != "" {
		return ParseAcceptLanguage(acceptLang)
	}

	// 1. Try to get language from user settings (if already loaded by TokenAuth or other middleware)
	if userSetting, ok := httpctx.GetContextKeyType[dto.UserSetting](c, constant.ContextKeyUserSetting); ok {
		if userSetting.Language != "" {
			normalized := normalizeLang(userSetting.Language)
			if IsSupported(normalized) {
				return normalized
			}
		}
	}

	// 2. Lazy load user language using user ID (for session-based auth where full settings aren't loaded)
	if userLangLoaderFunc != nil {
		if userId, exists := c.Get("id"); exists {
			if uid, ok := userId.(int); ok && uid > 0 {
				lang := userLangLoaderFunc(uid)
				if lang != "" {
					normalized := normalizeLang(lang)
					if IsSupported(normalized) {
						return normalized
					}
				}
			}
		}
	}

	// 3. Try to get language from context (set by I18n middleware from Accept-Language)
	if lang := c.GetString(string(constant.ContextKeyLanguage)); lang != "" {
		normalized := normalizeLang(lang)
		if IsSupported(normalized) {
			return normalized
		}
	}

	return DefaultLang
}

// ParseAcceptLanguage parses the Accept-Language header and returns the preferred language
func ParseAcceptLanguage(header string) string {
	if header == "" {
		return DefaultLang
	}

	// Simple parsing: take the first language tag
	parts := strings.Split(header, ",")
	if len(parts) == 0 {
		return DefaultLang
	}

	// Get the first language and remove quality value
	firstLang := strings.TrimSpace(parts[0])
	if idx := strings.Index(firstLang, ";"); idx > 0 {
		firstLang = firstLang[:idx]
	}

	return normalizeLang(firstLang)
}

// normalizeLang normalizes language code to supported format
func normalizeLang(lang string) string {
	lang = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(lang), "_", "-"))

	// Handle common variations
	switch {
	case strings.HasPrefix(lang, "zh-tw"), strings.HasPrefix(lang, "zh-hk"), strings.HasPrefix(lang, "zh-mo"), strings.HasPrefix(lang, "zh-hant"):
		return LangZhTW
	case strings.HasPrefix(lang, "zh"):
		return LangZhCN
	case strings.HasPrefix(lang, "en"):
		return LangEn
	case strings.HasPrefix(lang, "fr"):
		return LangFr
	case strings.HasPrefix(lang, "ru"):
		return LangRu
	case strings.HasPrefix(lang, "ja"):
		return LangJa
	case strings.HasPrefix(lang, "vi"):
		return LangVi
	default:
		return DefaultLang
	}
}

// SupportedLanguages returns a list of supported language codes
func SupportedLanguages() []string {
	return []string{LangZhCN, LangZhTW, LangEn, LangFr, LangRu, LangJa, LangVi}
}

// IsSupported checks if a language code is supported
func IsSupported(lang string) bool {
	lang = normalizeLang(lang)
	for _, supported := range SupportedLanguages() {
		if lang == supported {
			return true
		}
	}
	return false
}
