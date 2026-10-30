package textx

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMessageWithRequestIDReplacesTrailingRequestIDs(t *testing.T) {
	t.Parallel()

	message := "Context window is full. (request id: upstream-a) (request id: upstream-b)"
	require.Equal(t, "Context window is full. (request id: downstream)", MessageWithRequestID(message, "downstream"))
}
