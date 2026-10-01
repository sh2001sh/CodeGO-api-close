// Package identitydto preserves v2 management JSON at the identity boundary.
package identitydto

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const VersionHeader = "X-CodeGo-API-Version"

func IsV3(r *http.Request) bool { return r.Header.Get(VersionHeader) == "3" }

type responseBuffer struct {
	header http.Header
	status int
	bytes.Buffer
}

func (w *responseBuffer) Header() http.Header    { return w.header }
func (w *responseBuffer) WriteHeader(status int) { w.status = status }
func (w *responseBuffer) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.Buffer.Write(p)
}

func Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", VersionHeader)
		version := r.Header.Get(VersionHeader)
		if version != "" && version != "2" && version != "3" {
			writeError(w, http.StatusBadRequest, "unsupported API version")
			return
		}
		if IsV3(r) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			if err := adaptInput(r); err != nil {
				writeError(w, http.StatusBadRequest, "invalid legacy identity parameters")
				return
			}
		}
		capture := &responseBuffer{header: make(http.Header)}
		next.ServeHTTP(capture, r)
		payload := capture.Bytes()
		if strings.HasPrefix(capture.Header().Get("Content-Type"), "application/json") && capture.status < 400 {
			var envelope map[string]json.RawMessage
			if json.Unmarshal(payload, &envelope) == nil && envelope["data"] != nil {
				data, err := adaptOutput(r.URL.Path, envelope["data"])
				if err != nil {
					writeError(w, http.StatusConflict, "legacy identity response cannot represent this policy; request API version 3")
					return
				}
				envelope["data"] = data
				payload, err = json.Marshal(envelope)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "legacy identity serialization failed")
					return
				}
			}
		}
		for key, values := range capture.header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.Header().Del("Content-Length")
		if capture.status == 0 {
			capture.status = http.StatusOK
		}
		w.WriteHeader(capture.status)
		_, _ = w.Write(payload)
	})
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}{false, message})
}

func adaptInput(r *http.Request) error {
	key := r.URL.Path == "/api/token/"
	user := r.URL.Path == "/api/user/" || r.URL.Path == "/api/user/self" || strings.HasPrefix(r.URL.Path, "/api/user/") && !strings.Contains(strings.TrimPrefix(r.URL.Path, "/api/user/"), "/") && r.Method == http.MethodPut
	if (!key && !user) || r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return io.ErrUnexpectedEOF
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return io.ErrUnexpectedEOF
	}
	if key {
		err = keyInput(fields)
	} else {
		err = userInput(fields, r.URL.Path)
		if r.Method == http.MethodPut {
			delete(fields, "username")
		}
	}
	if err != nil {
		return err
	}
	data, err = json.Marshal(fields)
	if err != nil {
		return err
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	r.ContentLength = int64(len(data))
	return nil
}
