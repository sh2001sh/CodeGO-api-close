package live

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
)

// Only raster types that browsers cannot execute as a document are eligible
// for inline delivery. Client MIME metadata alone never grants that privilege.
func rasterFileMIME(content *os.File) (string, error) {
	var prefix [512]byte
	n, err := content.ReadAt(prefix[:], 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	switch detected := http.DetectContentType(prefix[:n]); detected {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return detected, nil
	default:
		return "", nil
	}
}

func fileContentHeaders(w http.ResponseWriter, file File, content *os.File) error {
	detected, err := rasterFileMIME(content)
	if err != nil {
		return err
	}
	disposition := "attachment"
	contentType := file.MIMEType
	if detected != "" {
		contentType, disposition = detected, "inline"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": file.Filename}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Defense in depth for direct navigation and saved active content: do not
	// grant scripts, application-origin access, frames or external subresources.
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	return nil
}

func (p *attachmentPreparation) validateImageReference(ctx context.Context, file *attachment) error {
	content, err := p.open(ctx, file)
	if err != nil {
		return err
	}
	defer func() { _ = content.Close() }()
	detected, err := rasterFileMIME(content)
	if err != nil {
		return err
	}
	declared, _, err := mime.ParseMediaType(file.file.MIMEType)
	if err != nil || detected == "" || (declared != detected && declared != "application/octet-stream") {
		return errors.New("live: image reference requires matching PNG, JPEG, GIF or WebP content")
	}
	file.imageMIME, file.file.MIMEType = detected, detected
	return nil
}
