package live

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"golang.org/x/sync/singleflight"
)

const attachmentUploadResponseLimit = 1 << 20

var attachmentUploads singleflight.Group

func attachmentMappingID(file File, req *gateway.Request, target gateway.Target) string {
	secret := sha256.Sum256([]byte(target.Secret))
	base := strings.TrimSpace(target.BaseURL)
	if parsed, err := url.Parse(base); err == nil {
		parsed.Scheme = strings.ToLower(parsed.Scheme)
		parsed.Host = strings.ToLower(parsed.Host)
		parsed.Path = strings.TrimRight(parsed.Path, "/")
		base = parsed.String()
	}
	baseHash := sha256.Sum256([]byte(base))
	headers, _ := json.Marshal(target.HeaderOverride)
	identityHash := sha256.Sum256(headers)
	data, _ := json.Marshal([]any{file.ID, file.SHA256, req.Principal.UserID, req.Principal.KeyID, target.ChannelID, target.CredentialID, target.Provider, req.Protocol, hex.EncodeToString(baseHash[:]), hex.EncodeToString(secret[:]), hex.EncodeToString(identityHash[:])})
	hash := sha256.Sum256(data)
	return "file-map-" + hex.EncodeToString(hash[:])
}

func validUpstreamFileID(id string) bool {
	if id == "" || len(id) > 512 || strings.HasPrefix(id, localFilePrefix) {
		return false
	}
	for _, char := range id {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' && char != '_' {
			return false
		}
	}
	return true
}

func (p *attachmentPreparation) native(ctx context.Context, file *attachment) (string, error) {
	if file.nativeID != "" {
		return file.nativeID, nil
	}
	if file.file.Size > p.limit {
		return "", ErrFileTooLarge
	}
	if p.h.cfg.Repository == nil || p.target.ChannelID <= 0 || p.target.CredentialID <= 0 || p.target.Secret == "" {
		return "", errors.New("live: native file routing is unavailable")
	}
	content, err := p.open(ctx, file)
	if err != nil {
		return "", err
	}
	defer func() { _ = content.Close() }()
	id := attachmentMappingID(file.file, p.req, p.target)
	ttl := p.h.cfg.LocatorTTL
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	locator := Locator{ID: id, UserID: p.req.Principal.UserID, KeyID: p.req.Principal.KeyID, ChannelID: p.target.ChannelID, CredentialID: p.target.CredentialID, CreatedAt: time.Now().UTC()}
	result := attachmentUploads.DoChan(id, func() (any, error) { return p.resolveNativeUpload(ctx, id, locator, file, content, ttl) })
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case outcome := <-result:
		return p.finishNativeUpload(ctx, outcome, &locator, file, ttl)
	}
}

// resolveNativeUpload is the singleflight-deduplicated body of native: it returns an existing
// cached upstream mapping if present and valid, otherwise uploads content and persists the new
// mapping. Only one caller per id actually runs this per attachmentUploads.DoChan semantics.
func (p *attachmentPreparation) resolveNativeUpload(ctx context.Context, id string, locator Locator, file *attachment, content *os.File, ttl time.Duration) (any, error) {
	cached, err := p.h.cfg.Repository.Get(ctx, id, locator.UserID, locator.KeyID)
	if err == nil {
		if cached.ID != id || cached.UserID != locator.UserID || cached.KeyID != locator.KeyID || cached.ChannelID != locator.ChannelID || cached.CredentialID != locator.CredentialID || !validUpstreamFileID(cached.UpstreamID) {
			return nil, errors.New("live: invalid native file mapping")
		}
		return cached.UpstreamID, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, errors.New("live: native file mapping lookup failed")
	}
	upstream, err := p.upload(ctx, file.file, content)
	if err != nil {
		return nil, err
	}
	locator.UpstreamID = upstream
	if err := p.h.cfg.Repository.Put(ctx, locator, ttl); err != nil {
		return nil, errors.New("live: native file mapping persistence failed")
	}
	return upstream, nil
}

// finishNativeUpload validates the singleflight outcome and, for callers that only observed a
// shared result (did not run resolveNativeUpload themselves), re-persists the mapping so their
// own locator fields (which DoChan does not propagate) are durably recorded too.
func (p *attachmentPreparation) finishNativeUpload(ctx context.Context, outcome singleflight.Result, locator *Locator, file *attachment, ttl time.Duration) (string, error) {
	if outcome.Err != nil {
		return "", outcome.Err
	}
	upstream, ok := outcome.Val.(string)
	if !ok || !validUpstreamFileID(upstream) {
		return "", errors.New("live: invalid native file mapping result")
	}
	if outcome.Shared {
		locator.UpstreamID = upstream
		if err := p.h.cfg.Repository.Put(ctx, *locator, ttl); err != nil {
			return "", errors.New("live: native file mapping persistence failed")
		}
	}
	file.nativeID = upstream
	return upstream, nil
}

func (p *attachmentPreparation) upload(ctx context.Context, file File, content *os.File) (string, error) {
	request, uploadCtx, cancel, err := p.buildNativeUploadRequest(ctx, file, content)
	if err != nil {
		return "", err
	}
	defer cancel()
	return p.sendNativeUploadRequest(uploadCtx, request)
}

// buildNativeUploadRequest resolves the target's native files endpoint and builds the
// multipart/form-data upload request for file's content, applying the same upstream header
// policy as a generation request (but never its generation ParamOverride).
func (p *attachmentPreparation) buildNativeUploadRequest(ctx context.Context, file File, content *os.File) (*http.Request, context.Context, context.CancelFunc, error) {
	base, err := url.Parse(strings.TrimSpace(p.target.BaseURL))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || base.Fragment != "" {
		return nil, nil, nil, errors.New("live: invalid native files endpoint")
	}
	base.Path = strings.TrimRight(base.Path, "/")
	if strings.HasSuffix(base.Path, "/v1") {
		base.Path += "/files"
	} else {
		base.Path += "/v1/files"
	}
	base.RawPath = ""
	var envelope bytes.Buffer
	writer := multipart.NewWriter(&envelope)
	purpose := strings.TrimSpace(file.Purpose)
	if purpose == "" {
		purpose = "user_data"
	}
	if err := writer.WriteField("purpose", purpose); err != nil {
		return nil, nil, nil, err
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": file.Filename}))
	header.Set("Content-Type", file.MIMEType)
	if _, err := writer.CreatePart(header); err != nil {
		return nil, nil, nil, err
	}
	prefixLength := envelope.Len()
	if err := writer.Close(); err != nil {
		return nil, nil, nil, err
	}
	prefix, suffix := envelope.Bytes()[:prefixLength], envelope.Bytes()[prefixLength:]
	body := io.MultiReader(bytes.NewReader(prefix), fileContextReader{ctx, io.NewSectionReader(content, 0, file.Size)}, bytes.NewReader(suffix))
	timeout := p.h.cfg.SessionTimeout
	if timeout <= 0 || timeout > 2*time.Minute {
		timeout = 2 * time.Minute
	}
	uploadCtx, cancel := context.WithTimeout(ctx, timeout)
	request, err := http.NewRequestWithContext(uploadCtx, http.MethodPost, base.String(), body)
	if err != nil {
		cancel()
		return nil, nil, nil, errors.New("live: invalid native files request")
	}
	request.ContentLength = int64(len(prefix)) + file.Size + int64(len(suffix))
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+p.target.Secret)
	// Generation parameters belong to the later JSON request, never this upload.
	uploadTarget := p.target
	uploadTarget.ParamOverride = nil
	if err := gateway.ApplyUpstreamRequest(request, p.req, uploadTarget); err != nil {
		cancel()
		return nil, nil, nil, err
	}
	return request, uploadCtx, cancel, nil
}

// sendNativeUploadRequest sends the prepared upload request and parses the upstream file ID
// from its JSON response.
func (p *attachmentPreparation) sendNativeUploadRequest(uploadCtx context.Context, request *http.Request) (string, error) {
	client, err := p.h.upstreamClient(uploadCtx, p.target)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		if uploadCtx.Err() != nil {
			return "", uploadCtx.Err()
		}
		return "", errors.New("live: native file upload failed")
	}
	defer func() { _ = response.Body.Close() }()
	response.StatusCode = gateway.MapUpstreamStatus(response.StatusCode, p.target)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("live: native files API returned HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, attachmentUploadResponseLimit+1))
	if err != nil {
		if uploadCtx.Err() != nil {
			return "", uploadCtx.Err()
		}
		return "", errors.New("live: native file upload response failed")
	}
	if len(raw) > attachmentUploadResponseLimit {
		return "", errors.New("live: native file upload response is too large")
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || !validUpstreamFileID(result.ID) {
		return "", errors.New("live: native file upload returned an invalid ID")
	}
	return result.ID, nil
}
