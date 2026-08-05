package stream

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type closeTrackingReader struct {
	closed bool
}

func (r *closeTrackingReader) Read(_ []byte) (int, error) {
	return 0, io.EOF
}

func (r *closeTrackingReader) Close() error {
	r.closed = true
	return nil
}

func TestCloseTimedOutStreamClosesResponseBody(t *testing.T) {
	body := &closeTrackingReader{}
	closeTimedOutStream(&http.Response{Body: body})
	require.True(t, body.closed)
}
