package http

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
)

func GetDesktopReleaseLatest(c *gin.Context) {
	manifest, err := platformruntime.LoadDesktopReleaseManifest()
	if err != nil {
		writeDesktopReleaseError(c, err)
		return
	}
	c.JSON(http.StatusOK, manifest)
}

func GetDesktopReleaseLatestJSON(c *gin.Context) {
	manifest, err := platformruntime.LoadDesktopUpdaterReleaseManifest()
	if err != nil {
		writeDesktopReleaseError(c, err)
		return
	}
	if len(manifest.Platforms) == 0 {
		writeDesktopReleaseError(c, errors.New("desktop release manifest is missing updater platforms"))
		return
	}

	c.JSON(http.StatusOK, platformruntime.DesktopUpdaterManifest{
		Version:   manifest.Version,
		Notes:     manifest.Notes,
		PubDate:   manifest.PublishedAt,
		Platforms: manifest.Platforms,
	})
}

func writeDesktopReleaseError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	message := "failed to load desktop release metadata"
	if errors.Is(err, platformruntime.ErrDesktopReleaseNotConfigured) {
		status = http.StatusServiceUnavailable
		message = err.Error()
	} else if err != nil {
		message = err.Error()
	}
	c.JSON(status, gin.H{
		"error":   message,
		"success": false,
	})
}
