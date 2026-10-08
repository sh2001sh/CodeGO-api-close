package live

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func (p *attachmentPreparation) reference(ctx context.Context, current map[string]any, parent string, file *attachment) error {
	if file == nil {
		return ErrNotFound
	}
	switch p.req.Protocol {
	case gateway.ProtocolAnthropic:
		return p.referenceAnthropic(ctx, current, parent, file)
	case gateway.ProtocolGemini:
		return p.referenceGemini(ctx, current, parent, file)
	case gateway.ProtocolOpenAIChat, gateway.ProtocolResponses:
		return p.referenceOpenAI(ctx, current, parent, file)
	default:
		return errors.New("live: local files are unsupported for this protocol")
	}
}

// referenceAnthropic rewrites an Anthropic "source" content block into an inline base64 file
// reference, since Anthropic has no file-ID or signed-URL reference shape.
func (p *attachmentPreparation) referenceAnthropic(ctx context.Context, current map[string]any, parent string, file *attachment) error {
	typ, _ := current["type"].(string)
	if parent != "source" || (typ != "file" && typ != "") {
		return errors.New("live: unsupported Anthropic local file reference")
	}
	if err := p.inline(ctx, file); err != nil {
		return err
	}
	if err := p.addBudget(int64(len(file.base64) + 6*len(file.file.MIMEType) + 64)); err != nil {
		return err
	}
	current["type"], current["media_type"], current["data"] = "base64", file.file.MIMEType, file.base64
	delete(current, "file_id")
	return nil
}

// referenceGemini rewrites a Gemini "fileData" content part into an inline base64 file
// reference, since Gemini has no file-ID or signed-URL reference shape here.
func (p *attachmentPreparation) referenceGemini(ctx context.Context, current map[string]any, parent string, file *attachment) error {
	if parent != "fileData" {
		return errors.New("live: unsupported Gemini local file reference")
	}
	if err := p.inline(ctx, file); err != nil {
		return err
	}
	if err := p.addBudget(int64(len(file.base64) + 6*len(file.file.MIMEType) + 64)); err != nil {
		return err
	}
	current["mimeType"], current["data"] = file.file.MIMEType, file.base64
	delete(current, "file_id")
	delete(current, "fileUri")
	return nil
}

// referenceOpenAI rewrites an OpenAI Chat/Responses content part into a native file ID (when
// uploading natively to OpenAI without a client-provided auth override), a signed delivery URL
// (when file delivery is configured), or an inline base64 data URL as the final fallback.
func (p *attachmentPreparation) referenceOpenAI(ctx context.Context, current map[string]any, parent string, file *attachment) error {
	typ, _ := current["type"].(string)
	nativeShape := typ == "input_image" || typ == "input_file" || parent == "file"
	urlShape := typ == "input_image" || typ == "input_file" || typ == "image_url" || parent == "image_url"
	if !nativeShape && !urlShape {
		return errors.New("live: unsupported local file reference shape")
	}
	nativeAuth := true
	for name := range p.target.HeaderOverride {
		if strings.EqualFold(name, "Authorization") {
			nativeAuth = false
		}
	}
	if nativeShape && p.target.Provider == "openai" && nativeAuth {
		id, err := p.native(ctx, file)
		if err != nil {
			return err
		}
		if err := p.addBudget(int64(len(id) * 6)); err != nil {
			return err
		}
		current["file_id"] = id
		return nil
	}
	value, signed, err := p.openAIFileValue(ctx, urlShape, file)
	if err != nil {
		return err
	}
	valueBudget := len(value) + 6*len(file.file.MIMEType)
	if signed {
		valueBudget = 6 * len(value)
	}
	if err := p.addBudget(int64(valueBudget + 6*len(file.file.Filename) + 64)); err != nil {
		return err
	}
	switch {
	case typ == "input_image":
		current["image_url"] = value
	case typ == "image_url":
		current["image_url"] = map[string]string{"url": value}
	case parent == "image_url":
		current["url"] = value
	case typ == "input_file" && signed:
		current["file_url"] = value
	default:
		current["file_data"] = value
		if _, exists := current["filename"]; !exists {
			current["filename"] = file.file.Filename
		}
	}
	delete(current, "file_id")
	return nil
}

// openAIFileValue resolves the URL/data value to embed for a non-native OpenAI file reference:
// a signed delivery URL when file delivery is configured for this reference shape, otherwise an
// inline base64 data URL.
func (p *attachmentPreparation) openAIFileValue(ctx context.Context, urlShape bool, file *attachment) (value string, signed bool, err error) {
	if urlShape && strings.TrimSpace(os.Getenv("FILE_DELIVERY_BASE_URL")) != "" {
		if file.signedURL == "" {
			if err := p.touch(ctx, file); err != nil {
				return "", false, err
			}
			var err error
			file.signedURL, err = BuildSignedFileDeliveryURL(os.Getenv("FILE_DELIVERY_BASE_URL"), file.file.ID, p.h.cfg.DeliveryKey, time.Now().UTC())
			if err != nil {
				return "", false, err
			}
		}
		return file.signedURL, true, nil
	}
	if err := p.inline(ctx, file); err != nil {
		return "", false, err
	}
	return file.dataURL, false, nil
}

func (p *attachmentPreparation) open(ctx context.Context, file *attachment) (*os.File, error) {
	metadata, content, err := p.h.cfg.Files.Open(ctx, p.req.Principal.UserID, file.file.ID)
	if err != nil {
		return nil, err
	}
	if content == nil {
		return nil, errors.New("live: local file content is unavailable")
	}
	if metadata.ID != file.file.ID || metadata.OwnerID != p.req.Principal.UserID || metadata.Size != file.file.Size || metadata.SHA256 != file.file.SHA256 {
		_ = content.Close()
		return nil, ErrNotFound
	}
	file.file = metadata
	if file.imageMIME != "" {
		file.file.MIMEType = file.imageMIME
	}
	return content, nil
}

func (p *attachmentPreparation) touch(ctx context.Context, file *attachment) error {
	content, err := p.open(ctx, file)
	if err != nil {
		return err
	}
	return content.Close()
}

func (p *attachmentPreparation) inline(ctx context.Context, file *attachment) error {
	if file.dataURL != "" {
		return nil
	}
	if file.file.Size > p.limit/4*3 {
		return ErrFileTooLarge
	}
	content, err := p.open(ctx, file)
	if err != nil {
		return err
	}
	defer func() { _ = content.Close() }()
	raw, err := io.ReadAll(io.LimitReader(fileContextReader{ctx, content}, file.file.Size+1))
	if err != nil {
		return err
	}
	if int64(len(raw)) != file.file.Size {
		return errors.New("live: local file content size changed")
	}
	file.base64 = base64.StdEncoding.EncodeToString(raw)
	file.dataURL = "data:" + file.file.MIMEType + ";base64," + file.base64
	return nil
}
