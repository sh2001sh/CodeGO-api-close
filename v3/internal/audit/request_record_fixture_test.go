package audit

import (
	"bytes"
	"io"
	"log/slog"
)

type blockedRequestLog struct {
	gate chan struct{}
	buf  bytes.Buffer
}

func (w *blockedRequestLog) Write(p []byte) (int, error) {
	<-w.gate
	return w.buf.Write(p)
}

func requestTestLog(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, nil))
}
