//go:build pgintegration

package legacy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/live"
	"github.com/tidwall/gjson"
)

// Unexpected execution ports count calls so history retrieval cannot silently
// plan/dispatch upstream work or create another debit.
type importedBackgroundPorts struct{ executionCalls int }

func (*importedBackgroundPorts) Authorize(_ context.Context, key string) (gateway.Principal, error) {
	if key == "owner" {
		return gateway.Principal{UserID: 7, KeyID: 11, Group: "default"}, nil
	}
	return gateway.Principal{UserID: 8, KeyID: 12, Group: "default"}, nil
}

func (p *importedBackgroundPorts) Plan(context.Context, *gateway.Request) ([]gateway.Target, error) {
	p.executionCalls++
	return nil, errors.New("unexpected history routing")
}

func (p *importedBackgroundPorts) Report(gateway.Target, gateway.AttemptResult) { p.executionCalls++ }

func (p *importedBackgroundPorts) Reserve(context.Context, *gateway.Request) error {
	p.executionCalls++
	return errors.New("unexpected history reservation")
}

func (p *importedBackgroundPorts) Finalize(context.Context, *gateway.Request, gateway.Outcome) error {
	p.executionCalls++
	return errors.New("unexpected history settlement")
}

func (p *importedBackgroundPorts) Put(context.Context, live.Locator, time.Duration) error {
	p.executionCalls++
	return errors.New("unexpected native locator write")
}

func (p *importedBackgroundPorts) Get(context.Context, string, int64, int64) (live.Locator, error) {
	p.executionCalls++
	return live.Locator{}, errors.New("unexpected native locator lookup")
}

func (p *importedBackgroundPorts) Delete(context.Context, string, int64, int64) error {
	p.executionCalls++
	return errors.New("unexpected native locator delete")
}

func verifyImportedBackgroundHTTP(t *testing.T, repository *live.RedisBackgroundRepository) {
	t.Helper()
	ports := &importedBackgroundPorts{}
	handler, err := live.New(live.Config{Auth: ports, Planner: ports, Settler: ports, Repository: ports, BackgroundJobs: repository,
		Resolve: func(context.Context, int64, int64) (gateway.Target, error) {
			ports.executionCalls++
			return gateway.Target{}, errors.New("unexpected history secret resolution")
		}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handler.Register(mux)
	for _, status := range []string{"completed", "failed", "cancelled"} {
		id := "resp_bg_legacy_" + status
		for _, path := range []string{"/v1/responses/" + id, "/responses/" + id + "?stream=true&starting_after=0"} {
			request := httptest.NewRequest("GET", path, nil)
			request.Header.Set("Authorization", "Bearer owner")
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			if response.Code != 200 {
				t.Fatalf("imported history unavailable from native HTTP: status %d", response.Code)
			}
			if strings.Contains(path, "stream=true") {
				if !strings.Contains(response.Body.String(), `"sequence_number":1`) || !strings.Contains(response.Body.String(), `"id":"`+id+`"`) || strings.Contains(response.Body.String(), "response.created") {
					t.Fatal("native HTTP resume ignored original cursor or public ID")
				}
			} else if gjson.GetBytes(response.Body.Bytes(), "id").Str != id || gjson.GetBytes(response.Body.Bytes(), "status").Str != status || !strings.Contains(response.Body.String(), "PRIVATE_RESULT") {
				t.Fatal("native HTTP result lost imported terminal facts")
			}
		}
		request := httptest.NewRequest("GET", "/v1/responses/"+id, nil)
		request.Header.Set("Authorization", "Bearer foreign")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != 404 {
			t.Fatal("foreign native HTTP caller accessed imported response")
		}
	}
	if ports.executionCalls != 0 {
		t.Fatal("native history retrieval billed or resolved upstream work")
	}
}
