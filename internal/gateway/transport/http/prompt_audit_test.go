package http

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gin-gonic/gin"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	securityaudit "github.com/sh2001sh/new-api/internal/gateway/securityaudit"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestBlockingPromptAuditRejectsRealtimeBeforeRelay(t *testing.T) {
	service := securityaudit.NewService(securityaudit.Config{
		Mode:   securityaudit.ModeBlocking,
		Groups: []string{"guarded"},
	}, nil)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	info := &relaycommon.RelayInfo{TokenGroup: "guarded", OriginModelName: "gpt-realtime"}

	err := checkPromptAuditWithService(ctx, types.RelayFormatOpenAIRealtime, nil, info, service)

	require.NotNil(t, err)
	require.Equal(t, types.ErrorCodePromptGuardUnavailable, err.GetErrorCode())
	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
}

func TestBlockingPromptAuditAllowsRealtimeOutsideConfiguredGroups(t *testing.T) {
	service := securityaudit.NewService(securityaudit.Config{
		Mode:   securityaudit.ModeBlocking,
		Groups: []string{"guarded"},
	}, nil)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	info := &relaycommon.RelayInfo{TokenGroup: "unrestricted", OriginModelName: "gpt-realtime"}

	err := checkPromptAuditWithService(ctx, types.RelayFormatOpenAIRealtime, nil, info, service)

	require.Nil(t, err)
}

func TestRelayPromptAuditPrecedesCostAndUpstreamSideEffects(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	positions := functionCallPositions(t, filepath.Join(filepath.Dir(currentFile), "relay_request.go"), "relayRequest")
	auditPosition, found := positions["checkPromptAudit"]
	require.True(t, found)

	for _, laterCall := range []string{
		"EstimateRequestToken",
		"PreConsumeRelayBilling",
		"ExecuteRealtimeRelay",
		"ExecuteClaudeRelay",
		"geminiRelayHandler",
		"relayHandler",
	} {
		position, exists := positions[laterCall]
		require.Truef(t, exists, "expected %s call in relayRequest", laterCall)
		require.Lessf(t, auditPosition, position, "prompt audit must precede %s", laterCall)
	}
}

func functionCallPositions(t *testing.T, filename, functionName string) map[string]token.Pos {
	t.Helper()
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, filename, nil, 0)
	require.NoError(t, err)
	positions := make(map[string]token.Pos)
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != functionName || function.Body == nil {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := callExpressionName(call.Fun)
			if name != "" {
				if _, exists := positions[name]; !exists {
					positions[name] = call.Pos()
				}
			}
			return true
		})
		return positions
	}
	t.Fatalf("function %s not found in %s", functionName, filename)
	return nil
}

func callExpressionName(expression ast.Expr) string {
	switch typed := expression.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return typed.Sel.Name
	default:
		return ""
	}
}
