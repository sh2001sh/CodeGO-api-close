package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

const attachmentMaxDepth = 64

const attachmentMaxFiles = 256

type attachment struct {
	file      File
	nativeID  string
	dataURL   string
	signedURL string
	base64    string
	imageMIME string
}

type attachmentPreparation struct {
	h          *Handler
	req        *gateway.Request
	target     gateway.Target
	files      map[string]*attachment
	limit      int64
	addedBytes int64
}

// PrepareFileReferences validates all local owners before any upload, then
// rewrites only the selected attempt's body. The caller retains req.Body for
// billing, retries, and audit; external upstream file IDs are preserved.
func (h *Handler) PrepareFileReferences(ctx context.Context, req *gateway.Request, target gateway.Target) ([]byte, error) {
	if req == nil {
		return nil, errors.New("live: file request is required")
	}
	if !bytes.Contains(req.Body, []byte(localFilePrefix)) {
		return req.Body, nil
	}
	if req.Principal.UserID <= 0 || req.Principal.KeyID <= 0 {
		return nil, errors.New("live: authenticated file owner is required")
	}
	limit := h.cfg.MaxBodyBytes
	if limit <= 0 {
		limit = 64 << 20
	}
	if int64(len(req.Body)) > limit {
		return nil, ErrFileTooLarge
	}
	decoder := json.NewDecoder(bytes.NewReader(req.Body))
	decoder.UseNumber()
	var root any
	if err := decoder.Decode(&root); err != nil {
		return nil, errors.New("live: invalid file-bearing JSON")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("live: trailing file-bearing JSON")
	}
	p := &attachmentPreparation{h: h, req: req, target: target, files: make(map[string]*attachment), limit: limit}
	if err := p.collect(ctx, root, "", 0); err != nil {
		return nil, err
	}
	if len(p.files) == 0 {
		return req.Body, nil
	}
	if err := p.rewrite(ctx, root, "", 0); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(root); err != nil {
		return nil, err
	}
	body := bytes.TrimSuffix(output.Bytes(), []byte("\n"))
	if int64(len(body)) > limit {
		return nil, ErrFileTooLarge
	}
	return body, nil
}

func (p *attachmentPreparation) collect(ctx context.Context, value any, parent string, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > attachmentMaxDepth {
		return errors.New("live: file request nesting is too deep")
	}
	switch current := value.(type) {
	case []any:
		for _, child := range current {
			if err := p.collect(ctx, child, parent, depth+1); err != nil {
				return err
			}
		}
	case map[string]any:
		if id, ok := current["file_id"].(string); ok && strings.HasPrefix(id, localFilePrefix) {
			if !validFileID(id) {
				return ErrNotFound
			}
			if _, exists := p.files[id]; !exists {
				if len(p.files) >= attachmentMaxFiles {
					return errors.New("live: too many local file references")
				}
				if p.h.cfg.Files == nil {
					return errors.New("live: file storage is unavailable")
				}
				file, err := p.h.cfg.Files.Get(ctx, p.req.Principal.UserID, id)
				if err != nil {
					return err
				}
				if file.ID != id || file.OwnerID != p.req.Principal.UserID || file.Size < 0 {
					return ErrNotFound
				}
				p.files[id] = &attachment{file: file}
			}
			typ, _ := current["type"].(string)
			if (p.req.Protocol == gateway.ProtocolOpenAIChat || p.req.Protocol == gateway.ProtocolResponses) &&
				(typ == "input_image" || typ == "image_url" || parent == "image_url") {
				if err := p.validateImageReference(ctx, p.files[id]); err != nil {
					return err
				}
			}
		}
		for key, child := range current {
			if err := p.collect(ctx, child, key, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *attachmentPreparation) rewrite(ctx context.Context, value any, parent string, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > attachmentMaxDepth {
		return errors.New("live: file request nesting is too deep")
	}
	switch current := value.(type) {
	case []any:
		for _, child := range current {
			if err := p.rewrite(ctx, child, parent, depth+1); err != nil {
				return err
			}
		}
	case map[string]any:
		if id, ok := current["file_id"].(string); ok && strings.HasPrefix(id, localFilePrefix) {
			if err := p.reference(ctx, current, parent, p.files[id]); err != nil {
				return err
			}
		}
		for key, child := range current {
			localGemini := false
			if p.req.Protocol == gateway.ProtocolGemini && key == "fileData" {
				if source, ok := child.(map[string]any); ok {
					id, _ := source["file_id"].(string)
					localGemini = strings.HasPrefix(id, localFilePrefix)
				}
			}
			if err := p.rewrite(ctx, child, key, depth+1); err != nil {
				return err
			}
			if localGemini {
				if _, exists := current["inlineData"]; exists {
					return errors.New("live: conflicting Gemini inline and local file references")
				}
				delete(current, key)
				current["inlineData"] = child
			}
		}
	}
	return nil
}

func (p *attachmentPreparation) addBudget(bytes int64) error {
	if bytes < 0 || bytes > p.limit-int64(len(p.req.Body))-p.addedBytes {
		return ErrFileTooLarge
	}
	p.addedBytes += bytes
	return nil
}
