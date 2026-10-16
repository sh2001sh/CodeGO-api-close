package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
)

func TestOwnerChannelReadModesKeepLegacyAndScopeHydration(t *testing.T) {
	engine := setupCreditPackFilterAPITest(t)
	db := platformdb.DB
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Settlement{}, &marketplaceschema.VerificationRun{}))
	require.NoError(t, db.Create(&marketplaceschema.Channel{
		ID: "read-owned", OwnerUserID: 42, SubmittedSourceLabel: "官方",
		BaseURLCiphertext: "private-url", CredentialCiphertext: "private-key",
		DeclaredModels: `["model-a"]`, ModelVerificationResults: `[]`,
	}).Error)
	require.NoError(t, db.Create(&marketplaceschema.Group{
		ID: "group-read-owned", ChannelID: "read-owned", OwnerUserID: 42,
		PublicSlug: "read-owned", InternalGroupName: "read-owned", Multiplier: 1,
		LifecycleStatus: "active", VerificationStatus: "passed",
	}).Error)
	engine.GET("/mine", func(c *gin.Context) { c.Set("id", 42); ListMyChannels(c) })
	for _, tc := range []struct {
		query   string
		length  int
		compact bool
	}{
		{"", 1, false},
		{"?view=index", 1, true},
		{"?ids=read-owned&ids=api-supported", 1, false},
		{"?ids=api-supported", 0, false},
		{"?ids=", 0, false},
	} {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/mine"+tc.query, nil))
		require.Equal(t, http.StatusOK, response.Code)
		var result struct {
			Success bool             `json:"success"`
			Data    []map[string]any `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		require.True(t, result.Success, response.Body.String())
		require.Len(t, result.Data, tc.length)
		require.NotContains(t, response.Body.String(), "private-key")
		if tc.length > 0 {
			require.Equal(t, "read-owned", result.Data[0]["id"])
			if tc.compact {
				require.Contains(t, result.Data[0], "search_text")
				require.NotContains(t, result.Data[0], "model_verification_results")
			} else {
				require.Contains(t, result.Data[0], "model_verification_results")
			}
		}
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/mine?"+strings.Repeat("ids=read-owned&", 51), nil))
	var rejected struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &rejected))
	require.False(t, rejected.Success)
	require.Contains(t, rejected.Message, "50")
}

func TestOwnerChannelQueueChecksOnlyOwnedLiveChannel(t *testing.T) {
	engine := setupCreditPackFilterAPITest(t)
	db := platformdb.DB
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Settlement{}, &marketplaceschema.VerificationRun{}))
	for _, id := range []string{"queue-live", "queue-deleted", "queue-error"} {
		channel := marketplaceschema.Channel{
			ID: id, OwnerUserID: 42, BaseURLCiphertext: "private", CredentialCiphertext: "private",
		}
		require.NoError(t, db.Create(&channel).Error)
		require.NoError(t, db.Create(&marketplaceschema.Group{
			ID: "group-" + id, ChannelID: id, OwnerUserID: 42,
			PublicSlug: id, InternalGroupName: id, Multiplier: 1,
		}).Error)
		if id == "queue-deleted" {
			require.NoError(t, db.Delete(&channel).Error)
		}
	}
	var calls []string
	engine.POST("/queue/:id", func(c *gin.Context) {
		c.Set("id", 42)
		queueOwnedChannelAction(c, func(id string) error {
			calls = append(calls, id)
			if id == "queue-error" {
				return errors.New("queue unavailable")
			}
			return nil
		})
	})
	for _, tc := range []struct {
		id      string
		success bool
		queued  bool
	}{
		{"queue-live", true, true},
		{"api-supported", false, false},
		{"queue-deleted", false, false},
		{"missing", false, false},
		{"queue-error", false, true},
	} {
		before := len(calls)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/queue/"+tc.id, nil))
		var result struct {
			Success bool `json:"success"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		require.Equal(t, tc.success, result.Success, response.Body.String())
		if tc.queued {
			require.Len(t, calls, before+1)
			require.Equal(t, tc.id, calls[before])
		} else {
			require.Len(t, calls, before)
		}
	}
}
