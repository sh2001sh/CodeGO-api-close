package live

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

type attachmentUploadTransportFunc func(*http.Request) (*http.Response, error)

func (f attachmentUploadTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestAttachmentsNativeUsesSelectedClientHeadersAndStatusMapping(t *testing.T) {
	for _, tc := range []struct {
		name           string
		status, mapped int
		wantSuccess    bool
	}{
		{"mapped-success", 503, 200, true},
		{"mapped-failure", 200, 503, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := filesTestStore(t)
			raw := []byte{0, 255, 128, 13, 10, 34}
			file := filesTestCreate(t, store, 11, raw)
			repo := &attachmentsRepository{}
			h := attachmentsHandler(t, store, repo)
			req := attachmentsRequest(file.ID, gateway.ProtocolResponses)
			req.ClientHeaders = map[string]string{"X-Route": "client-route", "Cookie": "private-cookie"}
			target := attachmentsTarget("https://selected-client.invalid/v1")
			target.ProxyURL = "unsupported://must-not-use-fallback-proxy"
			target.Fingerprint = gateway.CredentialFingerprint{UserAgent: "stable-account", TLSProfile: "selected-profile"}
			target.ParamOverride = map[string]any{"operations": "invalid-generation-operations", "purpose": "must-not-change-upload"}
			target.HeaderOverride = map[string]string{"X-Route": "{client_header:X-Route}", "X-Channel": "configured", "*": ""}
			target.StatusCodeMapping = map[string]int{strconv.Itoa(tc.status): tc.mapped}
			var resolves, uploads atomic.Int64
			h.cfg.Client = &http.Client{Transport: attachmentUploadTransportFunc(func(*http.Request) (*http.Response, error) {
				t.Error("fallback client used instead of the selected credential client")
				return nil, errors.New("fallback unavailable")
			})}
			selected := &http.Client{Transport: attachmentUploadTransportFunc(func(out *http.Request) (*http.Response, error) {
				uploads.Add(1)
				if out.Method != http.MethodPost || out.URL.String() != "https://selected-client.invalid/v1/files" {
					t.Errorf("configured-client upload endpoint: %s %s", out.Method, out.URL)
				}
				if out.Header.Get("X-Route") != "client-route" || out.Header.Get("X-Channel") != "configured" || out.Header.Get("Cookie") != "" || out.Header.Get("Authorization") != "Bearer "+target.Secret {
					t.Error("channel headers or protected client headers were applied incorrectly")
				}
				if err := out.ParseMultipartForm(1024); err != nil {
					t.Error(err)
					return nil, err
				}
				defer func() { _ = out.MultipartForm.RemoveAll() }()
				part, _, err := out.FormFile("file")
				if err != nil {
					t.Error(err)
					return nil, err
				}
				defer func() { _ = part.Close() }()
				got, err := io.ReadAll(part)
				if err != nil || !bytes.Equal(got, raw) || out.FormValue("purpose") != file.Purpose {
					t.Errorf("generation override changed multipart content: %x %v", got, err)
				}
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"file-selected"}`)), Request: out}, nil
			})}
			h.cfg.Clients = func(ctx context.Context, got gateway.Target) (*http.Client, error) {
				resolves.Add(1)
				if !reflect.DeepEqual(got, target) {
					t.Error("selected client did not receive the complete routing target")
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Error("client resolution did not inherit the bounded upload context")
				}
				return selected, nil
			}
			prepared, err := h.PrepareFileReferences(context.Background(), req, target)
			if resolves.Load() != 1 || uploads.Load() != 1 {
				t.Fatalf("configured client resolution=%d uploads=%d", resolves.Load(), uploads.Load())
			}
			if tc.wantSuccess {
				if err != nil || gjson.GetBytes(prepared, "input.0.file_id").Str != "file-selected" || len(repo.items) != 1 {
					t.Fatalf("mapped success: body=%s error=%v mappings=%d", prepared, err, len(repo.items))
				}
			} else if err == nil || !strings.Contains(err.Error(), "HTTP 503") || prepared != nil || len(repo.items) != 0 {
				t.Fatalf("mapped failure: body=%s error=%v mappings=%d", prepared, err, len(repo.items))
			}
		})
	}
}

func TestAttachmentsConfiguredClientFailureAndPolicyRejection(t *testing.T) {
	for _, mode := range []string{"client-error", "nil-client", "bad-header", "bad-status-mapping"} {
		t.Run(mode, func(t *testing.T) {
			store := filesTestStore(t)
			file := filesTestCreate(t, store, 11, []byte("file"))
			repo := &attachmentsRepository{}
			h := attachmentsHandler(t, store, repo)
			var resolves atomic.Int64
			h.cfg.Clients = func(context.Context, gateway.Target) (*http.Client, error) {
				resolves.Add(1)
				if mode == "nil-client" {
					return nil, nil
				}
				return nil, errors.New("selected client unavailable")
			}
			target := attachmentsTarget("https://unavailable.invalid")
			switch mode {
			case "bad-header":
				target.HeaderOverride = map[string]string{"X-Channel": "invalid\r\nvalue"}
			case "bad-status-mapping":
				target.StatusCodeMapping = map[string]int{"200": 999}
			}
			body, err := h.PrepareFileReferences(context.Background(), attachmentsRequest(file.ID, gateway.ProtocolResponses), target)
			if err == nil || body != nil || len(repo.items) != 0 {
				t.Fatalf("invalid client/policy continued: %s %v", body, err)
			}
			if strings.HasPrefix(mode, "bad-") && resolves.Load() != 0 {
				t.Fatal("invalid policy reached selected-client resolution")
			}
		})
	}
}

func TestAttachmentsConfiguredClientRedirectIsBlockedWithoutMutation(t *testing.T) {
	store := filesTestStore(t)
	file := filesTestCreate(t, store, 11, []byte("file"))
	var leaked, followed atomic.Int64
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		leaked.Add(1)
		_, _ = io.WriteString(w, `{"id":"file-leaked"}`)
	}))
	defer sink.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		http.Redirect(w, r, sink.URL, http.StatusTemporaryRedirect)
	}))
	defer up.Close()
	selected := up.Client()
	selected.CheckRedirect = func(*http.Request, []*http.Request) error { followed.Add(1); return nil }
	h := attachmentsHandler(t, store, &attachmentsRepository{})
	h.cfg.Clients = func(context.Context, gateway.Target) (*http.Client, error) { return selected, nil }
	if body, err := h.PrepareFileReferences(context.Background(), attachmentsRequest(file.ID, gateway.ProtocolResponses), attachmentsTarget(up.URL)); err == nil || body != nil {
		t.Fatalf("redirect upload accepted: %s %v", body, err)
	}
	if leaked.Load() != 0 || followed.Load() != 0 {
		t.Fatal("configured client's redirects exposed upload credentials")
	}
	if err := selected.CheckRedirect(nil, nil); err != nil || followed.Load() != 1 {
		t.Fatal("shared configured client's redirect policy was mutated")
	}
}
