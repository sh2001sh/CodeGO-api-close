package logger

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
)

func discardTestLogs(t *testing.T) {
	t.Helper()
	platformobservability.LogWriterMu.Lock()
	out, errors := gin.DefaultWriter, gin.DefaultErrorWriter
	gin.DefaultWriter, gin.DefaultErrorWriter = io.Discard, io.Discard
	platformobservability.LogWriterMu.Unlock()
	t.Cleanup(func() {
		platformobservability.LogWriterMu.Lock()
		gin.DefaultWriter, gin.DefaultErrorWriter = out, errors
		platformobservability.LogWriterMu.Unlock()
		logCount.Store(0)
		setupLogWorking.Store(false)
	})
}

func TestLogHelperConcurrentCount(t *testing.T) {
	discardTestLogs(t)
	logCount.Store(0)
	var workers sync.WaitGroup
	for i := 0; i < 32; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 64; j++ {
				LogInfo(context.Background(), "concurrent log")
				LogWarn(context.Background(), "concurrent warning")
			}
		}()
	}
	workers.Wait()
	if count := logCount.Load(); count != 4096 {
		t.Fatalf("concurrent log count=%d, want 4096", count)
	}
}

func TestLogHelperRotationCompletesWithoutLogDirectory(t *testing.T) {
	discardTestLogs(t)
	originalDir := *platformconfig.LogDir
	*platformconfig.LogDir = ""
	defer func() { *platformconfig.LogDir = originalDir }()
	logCount.Store(maxLogCount)
	setupLogWorking.Store(false)
	LogInfo(context.Background(), "rotation boundary")
	deadline := time.Now().Add(time.Second)
	for setupLogWorking.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if setupLogWorking.Load() || logCount.Load() != 0 {
		t.Fatal("rotation did not release its working flag and reset count")
	}
	LogWarn(context.Background(), "after rotation")
	if count := logCount.Load(); count != 1 {
		t.Fatalf("post-rotation count=%d, want 1", count)
	}
}
