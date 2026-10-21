package app

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateMarketplaceURLRejectsPrivateDestinations(t *testing.T) {
	t.Parallel()

	for _, target := range []string{
		"ftp://8.8.8.8", "http://127.0.0.1:8080", "http://10.0.0.1:65535",
		"https://127.0.0.1",
		"https://10.0.0.1",
		"https://169.254.169.254/latest/meta-data",
		"https://[::1]",
		"https://127.0.0.1:8080",
		"https://10.0.0.1:65535",
	} {
		t.Run(target, func(t *testing.T) {
			require.Error(t, ValidateMarketplaceURL(target))
		})
	}
}

func TestValidateMarketplaceURLAllowsPublicCustomPort(t *testing.T) {
	require.NoError(t, ValidateMarketplaceURL("https://178.83.203.233:8080/v1"))
	require.NoError(t, ValidateMarketplaceURL("http://178.83.203.233:8080/v1"))
}

func TestValidateMultiplierAllowsPositiveValuesWithinTechnicalLimit(t *testing.T) {
	require.NoError(t, validateMultiplier(0.0001))
	require.NoError(t, validateMultiplier(9999))
	require.NoError(t, validateMultiplier(maxMarketplaceMultiplier))
	require.Error(t, validateMultiplier(0))
	require.Error(t, validateMultiplier(-1))
	require.Error(t, validateMultiplier(maxMarketplaceMultiplier+1))
}

func TestValidateSourceLabel(t *testing.T) {
	require.NoError(t, validateSourceLabel("anthropic", "CC-Max"))
	require.NoError(t, validateSourceLabel("codex", "Plus"))
	require.NoError(t, validateSourceLabel("codex", "Codex Pro"))
	require.Error(t, validateSourceLabel("anthropic", ""))
	require.Error(t, validateSourceLabel("codex", "https://example.com"))
	require.Error(t, validateSourceLabel("codex", "加微信联系"))
	require.Error(t, validateSourceLabel("codex", "自营\n号池"))
	require.Error(t, validateSourceLabel("codex", "自营\u200b号池"))
	require.Error(t, validateSourceLabel("codex", "   "))
	for _, char := range []string{"A", "源", "🚀"} {
		require.NoError(t, validateSourceLabel("codex", "  "+strings.Repeat(char, 30)+"  "))
		require.ErrorContains(t, validateSourceLabel("codex", strings.Repeat(char, 31)), "30")
	}
	label, err := normalizeSourceLabel("  自营 Ｐｒｏ 号池 🚀  ")
	require.NoError(t, err)
	require.Equal(t, "自营 Pro 号池 🚀", label)
}

func TestSourceDeclarationRejectsLinks(t *testing.T) {
	for _, label := range []string{
		"https://a.example", "HTTP://a.test", "www.a.gg", "example.gg", "a.com.cn",
		"https://8.8.8.8:8080", "8.8.8.8:8080/v1", "8.8.8.8",
		"[来源](https://a.gg)", "自营 a.gg 号池", "h t t p s : / / a.gg",
		"example[.]gg", "ａｂｃ．ｇｇ", "例子.中国", "xn--fsq.xn--fiqs8s",
		"tg://join", "mailto:a@b.gg",
		"[2606:4700::1111]:8080", "2606:4700::1111", "https://[2606:4700::1111]",
	} {
		t.Run(label, func(t *testing.T) {
			require.EqualError(t, validateSourceLabel("codex", label), "来源声明不能包含链接、域名或 IP 地址")
		})
	}
	for _, label := range []string{"Codex Pro 自营号池", "GPT-5.4", "Claude 4.6", "GLM-4.7", "qwen3.5-plus", "国产模型", "自营号池 🚀"} {
		t.Run("allowed/"+label, func(t *testing.T) {
			require.NoError(t, validateSourceLabel("codex", label))
		})
	}
}
