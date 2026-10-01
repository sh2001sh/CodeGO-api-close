package live

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const fileDeliveryBucket = 5 * time.Minute

var ErrFileDeliveryToken = errors.New("live: invalid file delivery token")

func fileDeliveryTTL() (time.Duration, error) {
	minutes, err := fileEnvInt("FILE_DELIVERY_TTL_MINUTES", 15, false)
	if err != nil || minutes > 36500*24*60 {
		return 0, ErrFileDeliveryToken
	}
	return time.Duration(minutes) * time.Minute, nil
}

func (h *Handler) registerFiles(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/files", h.createFile)
	mux.HandleFunc("GET /v1/files", h.listFiles)
	mux.HandleFunc("GET /v1/files/{id}", h.getFile)
	mux.HandleFunc("DELETE /v1/files/{id}", h.deleteFile)
	mux.HandleFunc("GET /v1/files/{id}/content", h.fileContent)
	mux.HandleFunc("GET /v1/files/{id}/delivery", h.deliverFile)
}

func (h *Handler) filesAvailable(w http.ResponseWriter) bool {
	if h.cfg.Files == nil {
		writeError(w, 503, "file_storage_unavailable", "file storage is unavailable")
		return false
	}
	return true
}

func fileResponse(file File) map[string]any {
	response := map[string]any{"id": file.ID, "object": "file", "bytes": file.Size, "created_at": file.CreatedAt.Unix(), "filename": file.Filename, "purpose": file.Purpose, "status": "processed"}
	if file.ExpiresAt != nil {
		response["expires_at"] = file.ExpiresAt.Unix()
	}
	return response
}

func writeFileJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func (h *Handler) createFile(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.authorize(w, r)
	if !ok || !h.filesAvailable(w) {
		return
	}
	maxBytes, ok := h.parseFileUploadForm(w, r)
	if !ok {
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	if len(r.MultipartForm.File["file"]) != 1 {
		writeError(w, 400, "file_error", "exactly one file is required")
		return
	}
	header := r.MultipartForm.File["file"][0]
	content, err := header.Open()
	if err != nil {
		writeError(w, 400, "file_error", "unable to read file")
		return
	}
	defer func() { _ = content.Close() }()
	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	if _, _, err := mime.ParseMediaType(mimeType); err != nil {
		writeError(w, 400, "file_error", "invalid file content type")
		return
	}
	file, err := h.cfg.Files.Create(r.Context(), principal.UserID, header.Filename, r.FormValue("purpose"), mimeType, content, maxBytes)
	if err != nil {
		if errors.Is(err, ErrFileTooLarge) || errors.Is(err, ErrFileStorageFull) {
			writeError(w, 413, "file_error", "file size or storage limit exceeded")
		} else if errors.Is(err, ErrInvalidFileUpload) {
			writeError(w, 400, "file_error", "invalid file upload")
		} else {
			h.cfg.Logger.Error("file upload failed", "err", err)
			writeError(w, 500, "file_error", "unable to store file")
		}
		return
	}
	writeFileJSON(w, fileResponse(file))
}

// parseFileUploadForm validates the request has a multipart/form-data body within the
// configured size limit and parses it into r.MultipartForm. On success it returns the maximum
// file content size (excluding the small multipart envelope allowance) that the caller should
// pass to the file store.
func (h *Handler) parseFileUploadForm(w http.ResponseWriter, r *http.Request) (maxBytes int64, ok bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		writeError(w, 400, "file_error", "multipart/form-data is required")
		return 0, false
	}
	maxBytes = h.cfg.MaxBodyBytes
	if maxBytes <= 0 {
		maxBytes = 64 << 20
	}
	// Limit the multipart envelope as well as the actual persisted file. The
	// extra allowance lets a file at the limit carry its small multipart fields.
	if maxBytes > (1<<63-1)-(1<<20) {
		writeError(w, 503, "file_error", "invalid file size limit")
		return 0, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, 413, "file_error", "file too large")
		} else {
			writeError(w, 400, "file_error", "invalid multipart upload")
		}
		return 0, false
	}
	return maxBytes, true
}

func (h *Handler) listFiles(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.authorize(w, r)
	if !ok || !h.filesAvailable(w) {
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(w, 400, "file_error", "limit must be between 1 and 100")
			return
		}
		limit = parsed
	}
	files, more, err := h.cfg.Files.List(r.Context(), principal.UserID, limit, r.URL.Query().Get("after"))
	if err != nil {
		h.fileStoreError(w, err, "unable to list files")
		return
	}
	data := make([]map[string]any, 0, len(files))
	for _, file := range files {
		data = append(data, fileResponse(file))
	}
	writeFileJSON(w, map[string]any{"object": "list", "data": data, "has_more": more})
}

func (h *Handler) fileStoreError(w http.ResponseWriter, err error, message string) {
	if errors.Is(err, ErrNotFound) {
		writeError(w, 404, "file_error", "file not found")
		return
	}
	h.cfg.Logger.Error(message, "err", err)
	writeError(w, 500, "file_error", message)
}

func (h *Handler) getFile(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.authorize(w, r)
	if !ok || !h.filesAvailable(w) {
		return
	}
	file, err := h.cfg.Files.Get(r.Context(), principal.UserID, r.PathValue("id"))
	if err != nil {
		h.fileStoreError(w, err, "unable to read file metadata")
		return
	}
	writeFileJSON(w, fileResponse(file))
}

func (h *Handler) deleteFile(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.authorize(w, r)
	if !ok || !h.filesAvailable(w) {
		return
	}
	id := r.PathValue("id")
	if err := h.cfg.Files.Delete(r.Context(), principal.UserID, id); err != nil {
		h.fileStoreError(w, err, "unable to delete file")
		return
	}
	writeFileJSON(w, map[string]any{"id": id, "object": "file", "deleted": true})
}

func (h *Handler) fileContent(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.authorize(w, r)
	if !ok || !h.filesAvailable(w) {
		return
	}
	file, content, err := h.cfg.Files.Open(r.Context(), principal.UserID, r.PathValue("id"))
	if err != nil {
		h.fileStoreError(w, err, "unable to read file content")
		return
	}
	defer func() { _ = content.Close() }()
	w.Header().Set("Content-Type", file.MIMEType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("ETag", `"`+file.SHA256+`"`)
	http.ServeContent(w, r, file.Filename, file.CreatedAt, content)
}

func (h *Handler) deliverFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	now := time.Now().UTC()
	if h.cfg.Files == nil || VerifyFileDeliveryToken(id, r.URL.Query().Get("expires"), r.URL.Query().Get("signature"), h.cfg.DeliveryKey, now) != nil {
		w.WriteHeader(404)
		return
	}
	file, content, err := h.cfg.Files.OpenDelivery(r.Context(), id)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			h.cfg.Logger.Error("file delivery failed", "err", err)
		}
		w.WriteHeader(404)
		return
	}
	defer func() { _ = content.Close() }()
	expires, _ := strconv.ParseInt(r.URL.Query().Get("expires"), 10, 64)
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d, immutable", max(int64(0), expires-now.Unix())))
	w.Header().Set("ETag", `"`+file.SHA256+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", file.MIMEType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": file.Filename}))
	http.ServeContent(w, r, file.Filename, file.CreatedAt, content)
}

func signFileDelivery(id string, expires int64, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = fmt.Fprintf(mac, "%s\n%d", id, expires)
	return mac.Sum(nil)
}

// BuildSignedFileDeliveryURL uses the same HMAC payload and expiry buckets as
// v2. A signing key must contain at least 32 bytes; an unset key disables delivery.
func BuildSignedFileDeliveryURL(baseURL, id string, key []byte, now time.Time) (string, error) {
	if len(key) < 32 || !validFileID(id) {
		return "", ErrFileDeliveryToken
	}
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", errors.New("live: invalid file delivery base URL")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	ttl, err := fileDeliveryTTL()
	if err != nil {
		return "", err
	}
	expires := now.UTC().Truncate(fileDeliveryBucket).Add(ttl + fileDeliveryBucket).Unix()
	prefix := "/v1/files/"
	if strings.HasSuffix(strings.TrimRight(parsed.Path, "/"), "/v1") {
		prefix = "/files/"
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + prefix + id + "/delivery"
	query := parsed.Query()
	query.Set("expires", strconv.FormatInt(expires, 10))
	query.Set("signature", hex.EncodeToString(signFileDelivery(id, expires, key)))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func VerifyFileDeliveryToken(id, expiresRaw, signature string, key []byte, now time.Time) error {
	if len(key) < 32 || !validFileID(id) {
		return ErrFileDeliveryToken
	}
	ttl, err := fileDeliveryTTL()
	if err != nil {
		return err
	}
	expires, err := strconv.ParseInt(expiresRaw, 10, 64)
	if err != nil || expires <= now.Unix() || expires > now.Add(ttl+fileDeliveryBucket+time.Minute).Unix() {
		return ErrFileDeliveryToken
	}
	provided, err := hex.DecodeString(signature)
	if err != nil || !hmac.Equal(provided, signFileDelivery(id, expires, key)) {
		return ErrFileDeliveryToken
	}
	return nil
}
