package responsesws

import (
	"context"
	"time"

	"github.com/gorilla/websocket"
)

const (
	responsesWebSocketPingInterval = 30 * time.Second
	responsesWebSocketPongWait     = 90 * time.Second
)

func startResponsesWebSocketKeepAlive(ctx context.Context, conn *websocket.Conn) context.CancelFunc {
	keepAliveCtx, cancel := context.WithCancel(ctx)
	refreshDeadline := func() error {
		deadline := time.Now().Add(responsesWebSocketPongWait)
		if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
			deadline = contextDeadline
		}
		return conn.SetReadDeadline(deadline)
	}
	_ = refreshDeadline()
	conn.SetPongHandler(func(string) error { return refreshDeadline() })
	go func() {
		ticker := time.NewTicker(responsesWebSocketPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-keepAliveCtx.Done():
				return
			case <-ticker.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
					return
				}
			}
		}
	}()
	return cancel
}
