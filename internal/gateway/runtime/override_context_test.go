package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildParamOverrideContextIncludesNonPIIRouteIdentity(t *testing.T) {
	context := BuildParamOverrideContext(&RelayInfo{
		UserId:     12,
		UserGroup:  "standard",
		UsingGroup: "priority",
		TokenId:    34,
	})

	require.Equal(t, 12, context["user_id"])
	require.Equal(t, "standard", context["user_group"])
	require.Equal(t, "priority", context["using_group"])
	require.Equal(t, 34, context["token_id"])
	require.NotContains(t, context, "user_email")
	require.NotContains(t, context, "token_key")
}
