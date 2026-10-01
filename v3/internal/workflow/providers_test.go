package workflow

import (
	"context"
	"reflect"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func TestSnapshotResolverRestoresTaskChannelPolicyAndCredentialIdentity(t *testing.T) {
	channel := &catalog.Channel{ID: 7, Provider: "openai_video", BaseURL: "https://fixture.invalid", ProxyURL: "http://proxy.invalid", Scope: "marketplace", OwnerUserID: 99,
		Settings: map[string]any{"disable_store": true}, ParamOverride: map[string]any{"duration": 4}, HeaderOverride: map[string]string{"X-Policy": "configured"}, StatusCodeMapping: map[string]int{"503": 429},
		Credentials: []catalog.Credential{{ID: 8, Secret: "fixture", Fingerprint: catalog.CredentialFingerprint{UserAgent: "stable-task-identity", TLSProfile: "firefox"}}}}
	resolve := SnapshotResolver(func() *catalog.Snapshot { return &catalog.Snapshot{Channels: map[int64]*catalog.Channel{7: channel}} })
	target, err := resolve(context.Background(), 7, 8)
	if err != nil {
		t.Fatal(err)
	}
	if target.ChannelID != 7 || target.CredentialID != 8 || target.Provider != channel.Provider || target.BaseURL != channel.BaseURL || target.ProxyURL != channel.ProxyURL || target.Secret != "fixture" || target.Scope != "marketplace" || target.OwnerUserID != 99 {
		t.Fatal("task channel reference was not restored")
	}
	if !reflect.DeepEqual(target.Settings, channel.Settings) || !reflect.DeepEqual(target.ParamOverride, channel.ParamOverride) || !reflect.DeepEqual(target.HeaderOverride, channel.HeaderOverride) || !reflect.DeepEqual(target.StatusCodeMapping, channel.StatusCodeMapping) {
		t.Fatal("task channel policy was dropped")
	}
	if target.Fingerprint.UserAgent != "stable-task-identity" || target.Fingerprint.TLSProfile != "firefox" {
		t.Fatal("task credential fingerprint was dropped")
	}
	for _, ids := range [][2]int64{{1, 8}, {7, 2}} {
		if _, err := resolve(context.Background(), ids[0], ids[1]); err == nil {
			t.Fatal("missing credential/channel reference accepted")
		}
	}
	if _, err := SnapshotResolver(func() *catalog.Snapshot { return nil })(context.Background(), 7, 8); err == nil {
		t.Fatal("missing task catalog accepted")
	}
}
