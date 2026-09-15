package http

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGroupStatusRoutesIncludePublicAndAuthenticatedEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterUserRoutes(engine.Group("/api"), func(c *gin.Context) { c.Next() })

	want := map[string]bool{
		"GET /api/group-status":           false,
		"GET /api/user/self/group-status": false,
	}
	for _, route := range engine.Routes() {
		key := route.Method + " " + route.Path
		if _, exists := want[key]; exists {
			want[key] = true
		}
	}
	for route, registered := range want {
		require.Truef(t, registered, "route %s was not registered", route)
	}
}
