package runtime

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestRequestMayContainDisabledFields(t *testing.T) {
	defaultSettings := dto.ChannelOtherSettings{}

	for _, body := range [][]byte{
		[]byte(`{"model":"gpt-5","messages":[{"role":"user","content":"hello"}]}`),
		[]byte(`{"model":"gpt-5","input":"hello","stream":true}`),
	} {
		require.False(t, requestMayContainDisabledFields(body, defaultSettings))
	}

	tests := []struct {
		name     string
		body     string
		settings dto.ChannelOtherSettings
	}{
		{name: "service tier", body: `{"service_tier":"priority"}`},
		{name: "inference geo", body: `{"inference_geo":"us"}`},
		{name: "speed", body: `{"speed":"fast"}`},
		{name: "disabled store", body: `{"store":true}`, settings: dto.ChannelOtherSettings{DisableStore: true}},
		{name: "safety identifier", body: `{"safety_identifier":"user-1"}`},
		{name: "nested obfuscation", body: `{"stream_options":{"include_obfuscation":true}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.True(t, requestMayContainDisabledFields([]byte(tt.body), tt.settings))
		})
	}
	require.True(t, requestMayContainDisabledFields([]byte(`{"\u0073ervice_tier":"priority"}`), defaultSettings))

	require.False(t, requestMayContainDisabledFields(
		[]byte(`{"service_tier":"priority","store":true,"include_obfuscation":true}`),
		dto.ChannelOtherSettings{
			AllowServiceTier:        true,
			DisableStore:            false,
			AllowIncludeObfuscation: true,
		},
	))
}

func TestRemoveDisabledFieldsKeepsBodyWhenNoControlledFields(t *testing.T) {
	body := []byte(`{"model":"gpt-5","messages":[{"role":"user","content":"hello"}]}`)
	result, err := RemoveDisabledFields(body, dto.ChannelOtherSettings{}, false)
	require.NoError(t, err)
	require.Same(t, &body[0], &result[0])
	require.Equal(t, body, result)
}

func TestRemoveDisabledFieldsRemovesConfiguredFields(t *testing.T) {
	body := []byte(`{"model":"gpt-5","service_tier":"priority","store":true}`)
	result, err := RemoveDisabledFields(body, dto.ChannelOtherSettings{DisableStore: true}, false)
	require.NoError(t, err)
	require.NotContains(t, string(result), `"service_tier"`)
	require.NotContains(t, string(result), `"store"`)
	escaped, err := RemoveDisabledFields([]byte(`{"\u0073ervice_tier":"priority"}`), dto.ChannelOtherSettings{}, false)
	require.NoError(t, err)
	require.NotContains(t, string(escaped), "service_tier")
}

func TestRemoveDisabledFieldsPreservesLargeNestedInput(t *testing.T) {
	input := strings.Repeat("large conversation context ", 20_000)
	body := []byte(`{"model":"gpt-5","input":[{"role":"user","content":"` + input + `","service_tier":"nested"}],"\u0073ervice_tier":"priority","stream_options":{"include_obfuscation":false,"include_usage":true}}`)
	result, err := RemoveDisabledFields(body, dto.ChannelOtherSettings{}, false)
	require.NoError(t, err)
	var decoded map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(result, &decoded))
	require.NotContains(t, decoded, "service_tier")
	require.Contains(t, string(decoded["input"]), `"service_tier":"nested"`)
	require.Contains(t, string(decoded["input"]), input)
	require.JSONEq(t, `{"include_usage":true}`, string(decoded["stream_options"]))
}
