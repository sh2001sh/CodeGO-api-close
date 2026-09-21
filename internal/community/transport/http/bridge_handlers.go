package http

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	communityapp "github.com/sh2001sh/new-api/internal/community/app"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
)

func communityServiceAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authorization := strings.TrimSpace(c.GetHeader("Authorization"))
		token := ""
		if len(authorization) >= 7 && strings.EqualFold(authorization[:7], "Bearer ") {
			token = strings.TrimSpace(authorization[7:])
		}
		err := communityapp.AuthorizeCommunityService(token)
		if errors.Is(err, communityapp.ErrCommunityAPIDisabled) {
			writeCommunityBridgeError(c, http.StatusServiceUnavailable, "COMMUNITY_API_DISABLED", "community integration is not configured")
			c.Abort()
			return
		}
		if err != nil {
			c.Header("WWW-Authenticate", `Bearer realm="codego-community"`)
			writeCommunityBridgeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "community service authentication failed")
			c.Abort()
			return
		}
		c.Next()
	}
}

func getCommunityMember(c *gin.Context) {
	member, err := communityapp.GetCommunityMember(c.Param("sub"))
	if err != nil {
		writeCommunityBridgeAppError(c, err)
		return
	}
	writeCommunityBridgeSuccess(c, member)
}

func listCommunitySellers(c *gin.Context) {
	page, pageSize, err := parseCommunityBridgePagination(c)
	if err != nil {
		writeCommunityBridgeAppError(c, err)
		return
	}
	sellers, err := communityapp.ListCommunitySellers(page, pageSize, c.Query("keyword"), c.Query("provider"), c.Query("sort"))
	if err != nil {
		writeCommunityBridgeAppError(c, err)
		return
	}
	writeCommunityBridgeSuccess(c, sellers)
}

func listCommunityMemberChannels(c *gin.Context) {
	page, pageSize, err := parseCommunityBridgePagination(c)
	if err != nil {
		writeCommunityBridgeAppError(c, err)
		return
	}
	channels, err := communityapp.ListCommunityMemberChannels(
		c.Param("sub"), page, pageSize, c.Query("keyword"), c.Query("sort"), c.Query("viewer_sub"),
	)
	if err != nil {
		writeCommunityBridgeAppError(c, err)
		return
	}
	writeCommunityBridgeSuccess(c, channels)
}

func rateCommunityChannel(c *gin.Context) {
	var request communityapp.CommunityRatingRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeCommunityBridgeAppError(c, communityapp.ErrInvalidCommunityRating)
		return
	}
	result, err := communityapp.RateCommunityChannel(request, c.Param("id"))
	if err != nil {
		writeCommunityBridgeAppError(c, err)
		return
	}
	writeCommunityBridgeSuccess(c, result)
}

func parseCommunityBridgePagination(c *gin.Context) (int, int, error) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil {
		return 0, 0, communityapp.ErrInvalidCommunityPagination
	}
	pageSize, err := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err != nil || page < 1 || page > 10000 || pageSize < 1 || pageSize > 50 {
		return 0, 0, communityapp.ErrInvalidCommunityPagination
	}
	return page, pageSize, nil
}

func writeCommunityBridgeAppError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, communityapp.ErrInvalidCommunitySubject):
		writeCommunityBridgeError(c, http.StatusBadRequest, "INVALID_SUBJECT", "member subject is invalid")
	case errors.Is(err, communityapp.ErrInvalidCommunityPagination):
		writeCommunityBridgeError(c, http.StatusBadRequest, "INVALID_PAGINATION", "page must be positive and page_size must be between 1 and 50")
	case errors.Is(err, communityapp.ErrInvalidCommunityQuery):
		writeCommunityBridgeError(c, http.StatusBadRequest, "INVALID_QUERY", "community channel query is invalid")
	case errors.Is(err, communityapp.ErrInvalidCommunityRating):
		writeCommunityBridgeError(c, http.StatusBadRequest, "INVALID_RATING", "stars must be an integer between 1 and 5")
	case errors.Is(err, communityapp.ErrCommunityMemberNotFound):
		writeCommunityBridgeError(c, http.StatusNotFound, "MEMBER_NOT_FOUND", "community member was not found")
	case errors.Is(err, communityapp.ErrCommunityChannelNotFound):
		writeCommunityBridgeError(c, http.StatusNotFound, "CHANNEL_NOT_FOUND", "community channel was not found")
	case errors.Is(err, communityapp.ErrCommunityMemberInactive):
		writeCommunityBridgeError(c, http.StatusForbidden, "MEMBER_INACTIVE", "community member is inactive")
	case errors.Is(err, communityapp.ErrCommunitySelfRating):
		writeCommunityBridgeError(c, http.StatusForbidden, "SELF_RATING_FORBIDDEN", "channel owners cannot rate their own channels")
	default:
		platformobservability.SysError("community bridge request failed: " + err.Error())
		writeCommunityBridgeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "community integration request failed")
	}
}

func writeCommunityBridgeSuccess(c *gin.Context, data any) {
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

func writeCommunityBridgeError(c *gin.Context, status int, code, message string) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"success": false, "code": code, "message": message})
}
