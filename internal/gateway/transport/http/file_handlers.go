package http

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewayFiles "github.com/sh2001sh/new-api/internal/gateway/files"
	gatewaySchema "github.com/sh2001sh/new-api/internal/gateway/schema"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	"github.com/sh2001sh/new-api/types"
)

func currentFilePrincipal(c *gin.Context) (int, bool) {
	userID := c.GetInt("id")
	if _, exists := c.Get("role"); exists {
		return userID, c.GetInt("role") >= constant.RoleAdminUser
	}
	return userID, identityapp.IsUserAdmin(userID)
}

// DeliverFile serves an HMAC-authorized file URL to an upstream provider.
func DeliverFile(c *gin.Context) {
	id := c.Param("id")
	if err := gatewayFiles.VerifyDeliveryToken(id, c.Query("expires"), c.Query("signature"), time.Now().UTC()); err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	file, err := gatewayFiles.Get(id, 0, true)
	if err != nil || gatewayFiles.MarkUsed(file) != nil {
		c.Status(http.StatusNotFound)
		return
	}
	content, err := gatewayFiles.OpenContent(file)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	defer content.Close()
	info, err := content.Stat()
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	expires, _ := strconv.ParseInt(c.Query("expires"), 10, 64)
	maxAge := max(int64(0), expires-time.Now().Unix())
	c.Header("Cache-Control", fmt.Sprintf("public, max-age=%d, immutable", maxAge))
	c.Header("ETag", `"`+file.SHA256+`"`)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Type", file.MimeType)
	c.Header("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": file.Filename}))
	http.ServeContent(c.Writer, c.Request, file.Filename, info.ModTime(), content)
}

func writeFileError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": types.OpenAIError{Message: message, Type: "invalid_request_error", Code: "file_error"}})
}

func fileResponse(file *gatewaySchema.UserFile) gin.H {
	response := gin.H{
		"id": file.ID, "object": "file", "bytes": file.Size,
		"created_at": file.CreatedAt.Unix(), "filename": file.Filename,
		"purpose": file.Purpose, "status": "processed",
	}
	if expires := gatewayFiles.ExpiresAt(file); expires != nil {
		response["expires_at"] = expires.Unix()
	}
	return response
}

// CreateFile handles an OpenAI-compatible multipart file upload.
func CreateFile(c *gin.Context) {
	userID, _ := currentFilePrincipal(c)
	header := c.GetHeader("Content-Type")
	if !strings.Contains(header, "multipart/form-data") {
		writeFileError(c, http.StatusBadRequest, "multipart/form-data is required")
		return
	}
	purpose := c.PostForm("purpose")
	headerFile, err := c.FormFile("file")
	if err != nil {
		writeFileError(c, http.StatusBadRequest, "file is required")
		return
	}
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	opened, err := headerFile.Open()
	if err != nil {
		writeFileError(c, http.StatusBadRequest, "unable to read file")
		return
	}
	defer opened.Close()
	mimeType := headerFile.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	maxMB := constant.MaxFileDownloadMB
	if maxMB <= 0 {
		maxMB = 64
	}
	file, err := gatewayFiles.Create(userID, headerFile.Filename, purpose, mimeType, opened, int64(maxMB)<<20)
	if err != nil {
		if errors.Is(err, gatewayFiles.ErrTooLarge) || errors.Is(err, gatewayFiles.ErrStorageQuota) {
			writeFileError(c, http.StatusRequestEntityTooLarge, err.Error())
			return
		}
		writeFileError(c, http.StatusBadRequest, err.Error())
		return
	}
	c.JSON(http.StatusOK, fileResponse(file))
}

// ListFiles returns files visible to the authenticated principal.
func ListFiles(c *gin.Context) {
	userID, admin := currentFilePrincipal(c)
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, err := gatewayFiles.List(userID, admin, limit, c.Query("after"))
	if err != nil {
		writeFileError(c, http.StatusInternalServerError, "unable to list files")
		return
	}
	data := make([]gin.H, 0, len(items))
	for i := range items {
		data = append(data, fileResponse(&items[i]))
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": data, "has_more": false})
}

// GetFile returns OpenAI-compatible file metadata.
func GetFile(c *gin.Context) {
	userID, admin := currentFilePrincipal(c)
	file, err := gatewayFiles.Get(c.Param("id"), userID, admin)
	if errors.Is(err, gatewayFiles.ErrNotFound) {
		writeFileError(c, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		writeFileError(c, http.StatusInternalServerError, "unable to read file metadata")
		return
	}
	c.JSON(http.StatusOK, fileResponse(file))
}

// GetFileContent streams an authorized file's raw content.
func GetFileContent(c *gin.Context) {
	userID, admin := currentFilePrincipal(c)
	file, err := gatewayFiles.Get(c.Param("id"), userID, admin)
	if errors.Is(err, gatewayFiles.ErrNotFound) {
		writeFileError(c, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		writeFileError(c, http.StatusInternalServerError, "unable to read file metadata")
		return
	}
	if err := gatewayFiles.MarkUsed(file); err != nil {
		writeFileError(c, http.StatusNotFound, "file not found")
		return
	}
	content, err := gatewayFiles.OpenContent(file)
	if err != nil {
		writeFileError(c, http.StatusNotFound, "file content not found")
		return
	}
	defer content.Close()
	info, err := content.Stat()
	if err != nil {
		writeFileError(c, http.StatusInternalServerError, "unable to read file content")
		return
	}
	c.Header("Content-Type", file.MimeType)
	http.ServeContent(c.Writer, c.Request, file.Filename, info.ModTime(), content)
}

// DeleteFile removes an authorized file and its content.
func DeleteFile(c *gin.Context) {
	userID, admin := currentFilePrincipal(c)
	err := gatewayFiles.Delete(c.Param("id"), userID, admin)
	if errors.Is(err, gatewayFiles.ErrNotFound) {
		writeFileError(c, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		writeFileError(c, http.StatusInternalServerError, fmt.Sprintf("delete file: %v", err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "object": "file", "deleted": true})
}
