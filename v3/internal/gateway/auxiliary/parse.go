package auxiliary

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func geminiOperation(path string) Operation {
	if !strings.HasPrefix(path, "/v1/models/") && !strings.HasPrefix(path, "/v1beta/models/") {
		return ""
	}
	_, method, ok := strings.Cut(path, ":")
	if !ok {
		return ""
	}
	switch method {
	case "embedContent":
		return GeminiEmbed
	case "batchEmbedContents":
		return GeminiBatchEmbed
	case "predict":
		return GeminiImages
	}
	return ""
}

func operation(r *http.Request) Operation {
	if op := geminiOperation(r.URL.Path); op != "" {
		return op
	}
	if strings.HasPrefix(r.URL.Path, "/v1/engines/") && strings.HasSuffix(r.URL.Path, "/embeddings") {
		return Embeddings
	}
	for _, prefix := range []string{"/v1/", "/backend-api/codex/", "/"} {
		path := strings.TrimPrefix(r.URL.Path, prefix)
		for _, op := range []Operation{Images, ImageEdits, Edits, Embeddings, Transcriptions, Translations, Speech, Rerank, Moderations, Completions, Compact, Search} {
			if path == string(op) {
				return op
			}
		}
	}
	return ""
}

func (h *Handler) parse(w http.ResponseWriter, r *http.Request, req *gateway.Request) (Input, error) {
	in := Input{Operation: operation(r), Path: r.URL.Path, ContentType: r.Header.Get("Content-Type")}
	req.Path = r.URL.Path
	if in.Operation == "" || r.Method != http.MethodPost {
		return in, failure(404, "unknown_endpoint", "endpoint not found")
	}
	if err := parseRequestBody(w, r, req, &in, h.cfg.MaxBodyBytes); err != nil {
		return in, err
	}
	if err := resolveRequestModel(req, in); err != nil {
		return in, err
	}
	req.Stream = gjson.GetBytes(req.Body, "stream").Bool() || gjson.GetBytes(req.Body, "streaming").Bool()
	if in.Operation == Compact || in.Operation == Search {
		req.Stream = false
	}
	populateRequestHeaders(req, r, in)
	return in, nil
}

// parseRequestBody reads the client body (bounded by maxBodyBytes), decodes
// it as either multipart form data or a JSON object, and populates
// in.Body/req.Body accordingly.
func parseRequestBody(w http.ResponseWriter, r *http.Request, req *gateway.Request, in *Input, maxBodyBytes int64) error {
	var err error
	in.Body, err = io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return failure(413, "body_too_large", "request body too large")
		}
		return failure(400, "invalid_body", "could not read request body")
	}
	mediaType, params, _ := mime.ParseMediaType(in.ContentType)
	if mediaType == "multipart/form-data" {
		if in.Operation != ImageEdits && in.Operation != Transcriptions && in.Operation != Translations {
			return failure(400, "invalid_body", "endpoint does not accept multipart")
		}
		req.Body, err = multipartMetadata(in.Body, params["boundary"])
	} else {
		in.ContentType = "application/json"
		req.Body = in.Body
		if !gjson.ValidBytes(req.Body) || !gjson.ParseBytes(req.Body).IsObject() {
			err = errors.New("not an object")
		}
	}
	if err != nil {
		return failure(400, "invalid_body", "request must contain a valid JSON object or multipart form")
	}
	return nil
}

// resolveRequestModel derives the protocol and model name from the request
// path and body, applying each endpoint family's model-resolution rules.
func resolveRequestModel(req *gateway.Request, in Input) error {
	req.Protocol = gateway.ProtocolOpenAIChat
	req.Model = gjson.GetBytes(req.Body, "model").String()
	if in.Operation == Compact || in.Operation == Search {
		req.Protocol = gateway.ProtocolResponses
	}
	if op := geminiOperation(in.Path); op != "" {
		req.Protocol = gateway.ProtocolGemini
		segment := in.Path[strings.LastIndex(in.Path, "/")+1:]
		req.Model, _, _ = strings.Cut(segment, ":")
	}
	if req.Model == "" && strings.HasPrefix(in.Path, "/v1/engines/") {
		req.Model = strings.TrimSuffix(strings.TrimPrefix(in.Path, "/v1/engines/"), "/embeddings")
	}
	if req.Model == "" && in.Operation == Moderations {
		req.Model = "omni-moderation-latest"
	}
	if req.Model == "" {
		return failure(400, "missing_model", "model is required")
	}
	if in.Operation == Compact {
		req.Model = strings.TrimSuffix(req.Model, "-openai-compact")
	}
	return nil
}

// populateRequestHeaders copies pricing-relevant request headers (excluding
// credentials) and the small set of pass-through client headers onto req.
func populateRequestHeaders(req *gateway.Request, r *http.Request, in Input) {
	req.PricingHeaders = make(map[string]string)
	for name, values := range r.Header {
		switch strings.ToLower(name) {
		case "authorization", "x-api-key", "x-goog-api-key", "cookie", "proxy-authorization":
			continue
		}
		req.PricingHeaders[name] = strings.Join(values, ",")
	}
	req.PricingHeaders["X-Codego-Operation"] = string(in.Operation)
	for _, name := range []string{"OpenAI-Beta", "X-Codex-Turn-State"} {
		if value := r.Header.Get(name); value != "" {
			if req.ClientHeaders == nil {
				req.ClientHeaders = make(map[string]string)
			}
			req.ClientHeaders[name] = value
		}
	}
}

func multipartMetadata(body []byte, boundary string) ([]byte, error) {
	if boundary == "" {
		return nil, errors.New("missing multipart boundary")
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	fields := make(map[string]any)
	seen := make(map[string]bool)
	for count := 0; ; count++ {
		if count > 128 {
			return nil, errors.New("too many parts")
		}
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if err := addMultipartField(fields, seen, part); err != nil {
			return nil, err
		}
	}
	return json.Marshal(fields)
}

// addMultipartField reads one multipart form part and records its decoded
// value (or, for file parts, its byte size and derived audio duration) into
// fields, enforcing the metadata encoding's field-naming constraints.
func addMultipartField(fields map[string]any, seen map[string]bool, part *multipart.Part) error {
	data, err := io.ReadAll(part)
	if err != nil {
		return err
	}
	name := part.FormName()
	if name == "" {
		return errors.New("unnamed form part")
	}
	if seen[name] {
		return errors.New("duplicate form field")
	}
	seen[name] = true
	if strings.HasSuffix(name, "_bytes") || name == "audio_duration_micros" || name == "duration_micros" || name == "duration_seconds" || name == "duration" || name == "seconds" {
		return errors.New("reserved multipart accounting field")
	}
	if part.FileName() != "" {
		fields[name+"_bytes"] = len(data)
		if name == "file" {
			if duration := audioDuration(data, ""); duration > 0 {
				fields["audio_duration_micros"] = int64(duration * 1_000_000)
			}
		}
		return nil
	}
	if len(data) > 1<<20 {
		return errors.New("form field too large")
	}
	value := string(data)
	switch name {
	case "n":
		var number int
		if json.Unmarshal(data, &number) == nil {
			fields[name] = number
			return nil
		}
	case "stream":
		fields[name] = value == "true"
		return nil
	}
	fields[name] = value
	return nil
}
