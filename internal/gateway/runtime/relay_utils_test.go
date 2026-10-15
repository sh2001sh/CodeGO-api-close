package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJoinBaseURLPathNormalizesVersionPrefix(t *testing.T) {
	tests := []struct {
		name string
		base string
		path string
		want string
	}{
		{name: "base without slash", base: "https://provider.example", path: "/v1/chat/completions", want: "https://provider.example/v1/chat/completions"},
		{name: "base with slash", base: "https://provider.example/", path: "/v1/chat/completions", want: "https://provider.example/v1/chat/completions"},
		{name: "base already versioned", base: "https://provider.example/v1", path: "/v1/chat/completions", want: "https://provider.example/v1/chat/completions"},
		{name: "versioned base with slash", base: "https://provider.example/v1/", path: "v1/responses", want: "https://provider.example/v1/responses"},
		{name: "provider subpath", base: "https://provider.example/openai", path: "/v1/models", want: "https://provider.example/openai/v1/models"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, JoinBaseURLPath(tt.base, tt.path))
		})
	}
}
