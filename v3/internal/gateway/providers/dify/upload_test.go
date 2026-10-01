package dify

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

const pngBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+j2ioAAAAASUVORK5CYII="

func imageRequest() *gateway.Request {
	return chatRequest(`{"user":"image-user","messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"https://images.example/a.png"}},{"type":"image_url","image_url":{"url":"data:image/png;base64,`+pngBase64+`"}}]}]}`, false)
}

type countingTransport struct {
	base  http.RoundTripper
	count atomic.Int32
}

func (t *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.count.Add(1)
	return t.base.RoundTrip(req)
}

func TestInlineUploadAndChatUseInjectedChannelTransport(t *testing.T) {
	var uploads, chats atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("lost upload/chat authentication")
		}
		switch r.URL.Path {
		case "/tenant/v1/files/upload":
			uploads.Add(1)
			reader, err := r.MultipartReader()
			if err != nil {
				t.Error(err)
				return
			}
			field, err := reader.NextPart()
			if err != nil {
				t.Error(err)
				return
			}
			user, _ := io.ReadAll(field)
			if field.FormName() != "user" || string(user) != "image-user" {
				t.Errorf("bad upload user: %q", user)
			}
			file, err := reader.NextPart()
			if err != nil {
				t.Error(err)
				return
			}
			image, _ := io.ReadAll(file)
			expected, _ := base64.StdEncoding.DecodeString(pngBase64)
			if file.FormName() != "file" || file.FileName() != "image.png" || file.Header.Get("Content-Type") != "image/png" || !bytes.Equal(image, expected) {
				t.Error("inline bytes or MIME changed")
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"uploaded-file"}`)
		case "/tenant/v1/chat-messages":
			chats.Add(1)
			body, _ := io.ReadAll(r.Body)
			root := gjson.ParseBytes(body)
			if root.Get("files.0.transfer_mode").Str != "remote_url" || root.Get("files.1.transfer_mode").Str != "local_file" || root.Get("files.1.upload_file_id").Str != "uploaded-file" || root.Get("files.1.url").Exists() {
				t.Errorf("upload/remote image references lost: %s", body)
			}
			if strings.Contains(string(body), "base64") {
				t.Error("inline image leaked into Chat body")
			}
			_, _ = io.WriteString(w, `{"answer":"one image"}`)
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	p := Provider{}
	req := imageRequest()
	out, err := p.BuildRequest(context.Background(), req, gateway.Target{BaseURL: server.URL + "/tenant/v1", Secret: "test-token", ProxyURL: "http://proxy.invalid:1234"})
	if err != nil {
		t.Fatal(err)
	}
	if uploads.Load() != 0 || chats.Load() != 0 {
		t.Fatal("BuildRequest performed network I/O")
	}
	transport := &countingTransport{base: server.Client().Transport}
	resp, err := (&http.Client{Transport: p.UpstreamTransport(req, transport)}).Do(out)
	if err != nil {
		t.Fatal(err)
	}
	stream := p.Decode(req, resp)
	defer func() { _ = stream.Close() }()
	ev, err := stream.Next()
	if err != nil || ev.Kind != gateway.EventData || gjson.GetBytes(ev.Payload, "choices.0.message.content").Str != "one image" {
		t.Fatalf("failed after upload: %+v %v", ev, err)
	}
	if uploads.Load() != 1 || chats.Load() != 1 || transport.count.Load() != 2 {
		t.Fatalf("transport was bypassed: upload=%d chat=%d transport=%d", uploads.Load(), chats.Load(), transport.count.Load())
	}
}

func TestUploadFailureDoesNotSilentlyDropTheImage(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusCreated} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var chats atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/chat-messages") {
					chats.Add(1)
				}
				w.Header().Set("Retry-After", "5")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"message":"No image accepted"}`)
			}))
			defer server.Close()
			p := Provider{Client: server.Client()}
			out, err := p.BuildRequest(context.Background(), imageRequest(), gateway.Target{BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			resp, err := p.RoundTrip(out)
			if status == http.StatusTooManyRequests {
				if err != nil || resp.StatusCode != status || resp.Header.Get("Retry-After") != "5" {
					t.Fatalf("lost upload failure: %+v %v", resp, err)
				}
				_ = resp.Body.Close()
			} else if err == nil {
				t.Fatal("accepted upload without a file ID")
			}
			if chats.Load() != 0 {
				t.Fatal("Chat submitted after failed image upload")
			}
		})
	}
}

func TestUploadCancellationAndDeadline(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "canceled", true: "deadline"}[deadline], func(t *testing.T) {
			started := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				close(started)
				<-r.Context().Done()
			}))
			defer server.Close()
			var ctx context.Context
			var cancel context.CancelFunc
			if deadline {
				ctx, cancel = context.WithTimeout(context.Background(), time.Second)
			} else {
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			p := Provider{Client: server.Client()}
			out, err := p.BuildRequest(ctx, imageRequest(), gateway.Target{BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() { _, err := p.RoundTrip(out); result <- err }()
			<-started
			if !deadline {
				cancel()
			}
			err = <-result
			expected := context.Canceled
			if deadline {
				expected = context.DeadlineExceeded
			}
			if !errors.Is(err, expected) {
				t.Fatalf("upload lost context error: %v", err)
			}
		})
	}
}

func TestNestedHTTPClientsCannotFollowUploadOrChatRedirects(t *testing.T) {
	for _, inline := range []bool{false, true} {
		t.Run(map[bool]string{false: "chat", true: "upload"}[inline], func(t *testing.T) {
			var leaked atomic.Int32
			trap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1); w.WriteHeader(200) }))
			defer trap.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", trap.URL)
				w.WriteHeader(http.StatusTemporaryRedirect)
			}))
			defer server.Close()
			req := chatRequest(`{"messages":[{"role":"user","content":"x"}]}`, false)
			if inline {
				req = imageRequest()
			}
			p := Provider{Client: server.Client()}
			out, err := p.BuildRequest(context.Background(), req, gateway.Target{BaseURL: server.URL, Secret: "test-token"})
			if err != nil {
				t.Fatal(err)
			}
			resp, err := (&http.Client{Transport: p}).Do(out)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != 502 || leaked.Load() != 0 {
				t.Fatalf("redirect leaked request: status=%d leaked=%d", resp.StatusCode, leaked.Load())
			}
		})
	}
}

func TestInlineImageMIMEAndSizeValidation(t *testing.T) {
	for _, dataURL := range []string{
		"data:image/png;base64,AA==", "data:image/png;base64,not-base64", "data:image/svg+xml;base64,PHN2Zz4=", "data:image/jpeg;base64," + pngBase64,
		"data:image/png;base64," + strings.Repeat("A", base64.StdEncoding.EncodedLen(maxInlineImage)+1),
	} {
		req := chatRequest(`{"messages":[{"role":"user","content":[{"type":"text","text":"x"},{"type":"image_url","image_url":{"url":"`+dataURL+`"}}]}]}`, false)
		if _, err := (Provider{}).BuildRequest(context.Background(), req, gateway.Target{BaseURL: "https://api.example"}); err == nil {
			t.Fatal("accepted invalid inline image")
		}
	}
}
