package main

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/sync/errgroup"
)

func TestStoppedServiceCancelsSibling(t *testing.T) {
	group, ctx := errgroup.WithContext(context.Background())
	siblingStopped := make(chan struct{})
	group.Go(func() error {
		<-ctx.Done()
		close(siblingStopped)
		return ctx.Err()
	})
	group.Go(func() error {
		return runService(ctx, "ledger consumer", func(context.Context) error { return nil })
	})
	if err := group.Wait(); err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected stop should be reported: %v", err)
	}
	<-siblingStopped
}

func TestServicePreservesFailureAndShutdown(t *testing.T) {
	cause := errors.New("database disconnected")
	if err := runService(context.Background(), "relay", func(context.Context) error { return cause }); !errors.Is(err, cause) {
		t.Fatalf("lost service error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runService(ctx, "relay", func(context.Context) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost normal shutdown: %v", err)
	}
}
