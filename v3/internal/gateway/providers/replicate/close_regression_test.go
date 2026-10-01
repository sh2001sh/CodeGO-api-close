package replicate

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"
)

type blockedPredictionBody struct {
	entered chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (b *blockedPredictionBody) Read([]byte) (int, error) {
	close(b.entered)
	<-b.closed
	return 0, errors.New("http: read on closed response body")
}

func (b *blockedPredictionBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

// Regression: Close racing the initial prediction read must retain the
// cancellation classification, rather than report an unrelated body error.
func TestCloseDuringInitialReadReturnsCancellation(t *testing.T) {
	initial := &blockedPredictionBody{entered: make(chan struct{}), closed: make(chan struct{})}
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusCreated, Header: http.Header{}, Body: initial, Request: req}, nil
	})
	request := imageRequest(`{"prompt":"hello"}`)
	out, err := BuildImageRequest(context.Background(), request, target("https://api.example.invalid"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (Provider{}).WithTransport(transport).RoundTrip(out)
	if err != nil {
		t.Fatal(err)
	}
	stream := DecodeImages(request, resp)
	defer func() { _ = stream.Close() }()
	result := make(chan error, 1)
	go func() { _, readErr := stream.Next(); result <- readErr }()
	select {
	case <-initial.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("initial response read did not start")
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case readErr := <-result:
		if !errors.Is(readErr, context.Canceled) {
			t.Fatalf("Close lost cancellation: %v", readErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close failed to interrupt initial response read")
	}
}
