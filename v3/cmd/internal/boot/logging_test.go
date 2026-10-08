package boot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProcessLoggerPreservesActiveFileAndFinalizes(t *testing.T) {
	dir := t.TempDir()
	log, closeLog, err := ProcessLogger("control", dir)
	if err != nil {
		t.Fatal(err)
	}
	log.Info("logging fixture")
	active, err := filepath.Glob(filepath.Join(dir, "*.active.log"))
	if err != nil || len(active) != 1 {
		t.Fatalf("active logs = %v, error = %v", active, err)
	}
	data, err := os.ReadFile(active[0])
	if err != nil || !strings.Contains(string(data), `"msg":"logging fixture"`) {
		t.Fatalf("log record missing: %v", err)
	}
	if err = closeLog(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(active[0]); !os.IsNotExist(err) {
		t.Fatalf("active marker survived close: %v", err)
	}
	final, err := filepath.Glob(filepath.Join(dir, "*.log"))
	if err != nil || len(final) != 1 {
		t.Fatalf("final logs = %v, error = %v", final, err)
	}
}

func TestProcessLoggerRejectsFileDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing-file")
	if err := os.WriteFile(path, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ProcessLogger("control", path); err == nil {
		t.Fatal("non-directory accepted")
	}
	if _, _, err := ProcessLogger("../invalid", ""); err == nil {
		t.Fatal("invalid service accepted")
	}
}
