//go:build pgintegration

package main

import (
	"context"
	"strings"
	"testing"
)

func TestRestoreComposedFavoritesAndRootToolPermissions(t *testing.T) {
	s := restoreStack(t, nil)
	alice, bob := s.register(t, "restored_tools_alice"), s.register(t, "restored_tools_bob")
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `INSERT INTO v3_catalog.models(id,model_name) VALUES(11,'restored-favorite')`); err != nil {
		t.Fatal(err)
	}
	s.call(t, "GET", "/api/models/favorites", "", "", 401)
	s.call(t, "PUT", "/api/models/favorites", alice.AccessToken, `{"model_id":11,"favorite":true}`, 200)
	s.call(t, "PUT", "/api/models/favorites", alice.AccessToken, `{"model_id":11,"favorite":true}`, 200)
	w := s.call(t, "GET", "/api/models/favorites", bob.AccessToken, "", 200)
	if !strings.Contains(w.Body.String(), `"model_ids":[]`) {
		t.Fatalf("favorite leaked to other owner: %s", w.Body)
	}
	s.call(t, "PUT", "/api/models/favorites", bob.AccessToken, `{"model_id":11,"favorite":false}`, 200)
	w = s.call(t, "GET", "/api/models/favorites", alice.AccessToken, "", 200)
	if !strings.Contains(w.Body.String(), `"model_ids":[11]`) {
		t.Fatalf("other owner removed favorite: %s", w.Body)
	}
	s.call(t, "PUT", "/api/models/favorites", alice.AccessToken, `{"model_id":999,"favorite":true}`, 404)
	s.call(t, "PUT", "/api/models/favorites", alice.AccessToken, `{"model_id":0,"favorite":true}`, 400)
	for _, path := range []string{"/api/ratio_sync/channels", "/api/performance/stats", "/api/deployments/settings"} {
		s.call(t, "GET", path, alice.AccessToken, "", 403)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE v3_identity.users SET role='admin' WHERE id=$1`, alice.User.ID); err != nil {
		t.Fatal(err)
	}
	s.call(t, "GET", "/api/deployments/settings", alice.AccessToken, "", 200)
	s.call(t, "POST", "/api/deployments/settings/test-connection", alice.AccessToken, `{"api_key":"private-replacement"}`, 403)
	s.call(t, "GET", "/api/performance/stats", alice.AccessToken, "", 403)
	s.call(t, "GET", "/api/ratio_sync/channels", alice.AccessToken, "", 403)
	// The generic settings route must not bypass the tools' root-only credentials.
	s.call(t, "PUT", "/api/settings/model_deployment.ionet.api_key", alice.AccessToken, `{"value":"private-replacement","sensitive":true}`, 403)
	if _, err := s.pool.Exec(ctx, `UPDATE v3_identity.users SET role='root' WHERE id=$1`, alice.User.ID); err != nil {
		t.Fatal(err)
	}
	s.call(t, "GET", "/api/performance/stats", alice.AccessToken, "", 200)
	s.call(t, "GET", "/api/ratio_sync/channels", alice.AccessToken, "", 200)
	s.call(t, "POST", "/api/ratio_sync/fetch", alice.AccessToken, `{`, 400)
	s.call(t, "DELETE", "/api/performance/disk_cache", alice.AccessToken, "", 409)
	s.call(t, "DELETE", "/api/performance/logs?mode=by_days&value=30", alice.AccessToken, "", 409)
	for _, path := range []string{"/api/user/miniprogram/binding", "/api/miniprogram/login", "/api/miniprogram/dashboard"} {
		s.call(t, "GET", path, alice.AccessToken, "", 404)
	}
}
