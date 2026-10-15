package contract

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHasRemoteCompactionV2(t *testing.T) {
	headers := http.Header{"X-Codex-Beta-Features": []string{"foo, remote_compaction_v2, bar"}}
	require.True(t, HasRemoteCompactionV2(headers))
	require.False(t, HasRemoteCompactionV2(http.Header{"X-Codex-Beta-Features": []string{"remote_compaction_v2_preview"}}))
}
