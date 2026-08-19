package projection

import (
	"testing"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	"github.com/stretchr/testify/require"
)

func TestFormatUserLogsHidesOwnerOnlyDiagnostics(t *testing.T) {
	logs := []*auditschema.Log{{
		Other: `{"owner_error":"upstream account balance","admin_info":{"route":"secret"},"retry_count":2}`,
	}}

	formatUserLogs(logs, 0)

	require.NotContains(t, logs[0].Other, "owner_error")
	require.NotContains(t, logs[0].Other, "upstream account balance")
	require.NotContains(t, logs[0].Other, "admin_info")
	require.Contains(t, logs[0].Other, "retry_count")
}
