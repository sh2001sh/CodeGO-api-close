package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	fastjson "github.com/goccy/go-json"
	"github.com/sh2001sh/new-api/constant"
)

const KeyRequestBody = "key_request_body"
const KeyBodyStorage = "key_body_storage"
const KeyBodyTiming = "gateway_body_timing"
const KeyRequestBodySnapshot = "key_request_body_snapshot"

// RequestBodySnapshot contains the small set of JSON fields needed by routing.
// It avoids decoding a large request once for model selection and again for
// request-profile hints. Raw remains owned by BodyStorage and is not copied for
// memory-backed bodies.
type RequestBodySnapshot struct {
	Raw                []byte          `json:"-"`
	Model              string          `json:"model"`
	Stream             *bool           `json:"stream"`
	StreamOptions      json.RawMessage `json:"stream_options"`
	ServiceTier        json.RawMessage `json:"service_tier"`
	Store              json.RawMessage `json:"store"`
	SafetyIdentifier   json.RawMessage `json:"safety_identifier"`
	InferenceGeo       json.RawMessage `json:"inference_geo"`
	Speed              json.RawMessage `json:"speed"`
	Tools              json.RawMessage `json:"tools"`
	Functions          json.RawMessage `json:"functions"`
	PromptCacheKey     json.RawMessage `json:"prompt_cache_key"`
	PreviousResponseID string          `json:"previous_response_id"`
	Conversation       json.RawMessage `json:"conversation"`
}

// bodyTimingRecorder is intentionally structural to keep the HTTP body
// package independent from the gateway runtime package.
type bodyTimingRecorder interface {
	MarkBodyReadStarted()
	MarkBodyReadDone()
	MarkJSONDecodeStarted()
	MarkJSONDecodeDone()
}

func bodyTiming(c *gin.Context) bodyTimingRecorder {
	if c == nil {
		return nil
	}
	value, exists := c.Get(KeyBodyTiming)
	if !exists {
		return nil
	}
	recorder, _ := value.(bodyTimingRecorder)
	return recorder
}

var ErrRequestBodyTooLarge = errors.New("request body too large")

func IsRequestBodyTooLargeError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrRequestBodyTooLarge) {
		return true
	}
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}

func GetRequestBody(c *gin.Context) (io.Seeker, error) {
	if storage, exists := c.Get(KeyBodyStorage); exists && storage != nil {
		if bs, ok := storage.(BodyStorage); ok {
			if _, err := bs.Seek(0, io.SeekStart); err != nil {
				return nil, fmt.Errorf("failed to seek body storage: %w", err)
			}
			return bs, nil
		}
	}

	if cached, exists := c.Get(KeyRequestBody); exists && cached != nil {
		if bodyBytes, ok := cached.([]byte); ok {
			bs, err := CreateBodyStorage(bodyBytes)
			if err != nil {
				return nil, err
			}
			c.Set(KeyBodyStorage, bs)
			return bs, nil
		}
	}

	maxMB := constant.MaxRequestBodyMB
	if maxMB <= 0 {
		maxMB = 128
	}
	maxBytes := int64(maxMB) << 20

	if timing := bodyTiming(c); timing != nil {
		timing.MarkBodyReadStarted()
		defer timing.MarkBodyReadDone()
	}
	storage, err := CreateBodyStorageFromReader(c.Request.Body, c.Request.ContentLength, maxBytes)
	_ = c.Request.Body.Close()
	if err != nil {
		if IsRequestBodyTooLargeError(err) {
			return nil, fmt.Errorf("%w: request body exceeds %d MB", ErrRequestBodyTooLarge, maxMB)
		}
		return nil, err
	}

	c.Set(KeyBodyStorage, storage)
	return storage, nil
}

func GetBodyStorage(c *gin.Context) (BodyStorage, error) {
	seeker, err := GetRequestBody(c)
	if err != nil {
		return nil, err
	}
	storage, ok := seeker.(BodyStorage)
	if !ok {
		return nil, errors.New("unexpected body storage type")
	}
	return storage, nil
}

func CleanupBodyStorage(c *gin.Context) {
	if storage, exists := c.Get(KeyBodyStorage); exists && storage != nil {
		if bs, ok := storage.(BodyStorage); ok {
			_ = bs.Close()
		}
		c.Set(KeyBodyStorage, nil)
	}
	c.Set(KeyRequestBodySnapshot, nil)
}

// GetRequestBodySnapshot parses routing metadata once for a JSON request.
// Non-JSON callers receive an empty snapshot without changing existing form or
// multipart behavior.
func GetRequestBodySnapshot(c *gin.Context) (*RequestBodySnapshot, error) {
	if c == nil || !strings.HasPrefix(c.Request.Header.Get("Content-Type"), "application/json") {
		return &RequestBodySnapshot{}, nil
	}
	if cached, exists := c.Get(KeyRequestBodySnapshot); exists && cached != nil {
		if snapshot, ok := cached.(*RequestBodySnapshot); ok {
			return snapshot, nil
		}
	}
	storage, err := GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	snapshot := &RequestBodySnapshot{}
	if storage.IsDisk() {
		if _, err := storage.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		if err := fastjson.NewDecoder(storage).Decode(snapshot); err != nil {
			return nil, err
		}
		if _, err := storage.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
	} else {
		raw, err := storage.Bytes()
		if err != nil {
			return nil, err
		}
		snapshot.Raw = raw
		if err := fastjson.Unmarshal(raw, snapshot); err != nil {
			return nil, err
		}
	}
	c.Set(KeyRequestBodySnapshot, snapshot)
	return snapshot, nil
}

func UnmarshalBodyReusable(c *gin.Context, v any) error {
	storage, err := GetBodyStorage(c)
	if err != nil {
		return err
	}

	contentType := c.Request.Header.Get("Content-Type")
	timing := bodyTiming(c)
	if timing != nil {
		timing.MarkJSONDecodeStarted()
		defer timing.MarkJSONDecodeDone()
	}
	if storage.IsDisk() && strings.HasPrefix(contentType, "application/json") {
		if _, err := storage.Seek(0, io.SeekStart); err != nil {
			return err
		}
		if err := fastjson.NewDecoder(storage).Decode(v); err != nil {
			return err
		}
		if _, err := storage.Seek(0, io.SeekStart); err != nil {
			return err
		}
		c.Request.Body = io.NopCloser(storage)
		return nil
	}

	requestBody, err := storage.Bytes()
	if err != nil {
		return err
	}

	switch {
	case strings.HasPrefix(contentType, "application/json"):
		err = fastjson.Unmarshal(requestBody, v)
	case strings.Contains(contentType, gin.MIMEPOSTForm):
		err = parseFormData(requestBody, v)
	case strings.Contains(contentType, gin.MIMEMultipartPOSTForm):
		err = parseMultipartFormData(c, requestBody, v)
	default:
	}
	if err != nil {
		return err
	}

	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return err
	}
	c.Request.Body = io.NopCloser(storage)
	return nil
}

func ParseMultipartFormReusable(c *gin.Context) (*multipart.Form, error) {
	storage, err := GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	requestBody, err := storage.Bytes()
	if err != nil {
		return nil, err
	}

	contentType := getOriginalMultipartContentType(c)
	boundary, err := parseBoundary(contentType)
	if err != nil {
		return nil, err
	}

	reader := multipart.NewReader(bytes.NewReader(requestBody), boundary)
	form, err := reader.ReadForm(multipartMemoryLimit())
	if err != nil {
		return nil, err
	}

	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	c.Request.Body = io.NopCloser(storage)
	return form, nil
}

func getOriginalMultipartContentType(c *gin.Context) string {
	if saved, ok := c.Get("_original_multipart_ct"); ok {
		return saved.(string)
	}
	contentType := c.Request.Header.Get("Content-Type")
	c.Set("_original_multipart_ct", contentType)
	return contentType
}

func processFormMap(formMap map[string]any, v any) error {
	jsonData, err := json.Marshal(formMap)
	if err != nil {
		return err
	}
	return json.Unmarshal(jsonData, v)
}

func parseFormData(data []byte, v any) error {
	values, err := url.ParseQuery(string(data))
	if err != nil {
		return err
	}

	formMap := make(map[string]any, len(values))
	for key, vals := range values {
		if len(vals) == 1 {
			formMap[key] = vals[0]
			continue
		}
		formMap[key] = vals
	}
	return processFormMap(formMap, v)
}

func parseMultipartFormData(c *gin.Context, data []byte, v any) error {
	boundary, err := parseBoundary(getOriginalMultipartContentType(c))
	if err != nil {
		if errors.Is(err, errBoundaryNotFound) {
			return json.Unmarshal(data, v)
		}
		return err
	}

	reader := multipart.NewReader(bytes.NewReader(data), boundary)
	form, err := reader.ReadForm(multipartMemoryLimit())
	if err != nil {
		return err
	}
	defer form.RemoveAll()

	formMap := make(map[string]any, len(form.Value))
	for key, vals := range form.Value {
		if len(vals) == 1 {
			formMap[key] = vals[0]
			continue
		}
		formMap[key] = vals
	}
	return processFormMap(formMap, v)
}

var errBoundaryNotFound = errors.New("multipart boundary not found")

func parseBoundary(contentType string) (string, error) {
	if contentType == "" {
		return "", errBoundaryNotFound
	}
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", err
	}
	boundary, ok := params["boundary"]
	if !ok || boundary == "" {
		return "", errBoundaryNotFound
	}
	return boundary, nil
}

func multipartMemoryLimit() int64 {
	limitMB := constant.MaxFileDownloadMB
	if limitMB <= 0 {
		limitMB = 32
	}
	return int64(limitMB) << 20
}
