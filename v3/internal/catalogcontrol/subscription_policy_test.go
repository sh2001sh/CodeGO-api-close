package catalogcontrol

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInvalidSubscriptionPolicyRejectedBeforeDatabase(t *testing.T) {
	mux := http.NewServeMux()
	New(nil, nil, nil).Register(mux, func(h http.Handler) http.Handler { return h })
	for _, endpoint := range []struct{ method, path, body string }{
		{"PUT", "/api/settings/SubscriptionGroupPolicy", `{"value":{"default":{"enabled":true,"multiplier":-1}}}`},
		{"PUT", "/api/option/", `{"key":"SubscriptionGroupPolicy","value":"{\"default\":{\"enabled\":true,\"multiplier\":0}}"}`},
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(endpoint.body)))
		if w.Code != 400 {
			t.Fatalf("invalid policy reached persistence: %d %s", w.Code, w.Body.String())
		}
	}
}
