package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/riverqueue/river"
)

func TestMaintenanceRejectsUnknownActionAndInvalidSchedule(t *testing.T) {
	w := &maintenanceWorker{actions: map[string]func(context.Context) error{}}
	if err := w.Work(context.Background(), &river.Job[maintenanceArgs]{Args: maintenanceArgs{Action: "unknown"}}); err == nil {
		t.Fatal("unsupported persisted action must be canceled")
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for name, period := range map[string]time.Duration{"missing": time.Minute, "too_fast": time.Millisecond} {
		_, err := newJobs(nil, map[string]func(context.Context) error{"too_fast": func(context.Context) error { return errors.New("unused") }}, map[string]time.Duration{name: period}, logger)
		if err == nil {
			t.Fatalf("invalid schedule %s accepted", name)
		}
	}
}
