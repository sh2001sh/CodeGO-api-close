package boot

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// ProcessLogger keeps container output and, when requested, protected active
// files for the restored administrative log listing and retention tools.
func ProcessLogger(service, directory string) (*slog.Logger, func() error, error) {
	if service != "control" && service != "gateway" && service != "worker" {
		return nil, nil, errors.New("invalid logging service")
	}
	if directory == "" {
		return slog.New(slog.NewJSONHandler(os.Stderr, nil)), func() error { return nil }, nil
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, nil, fmt.Errorf("create log directory: %w", err)
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, errors.New("log directory must be a regular directory")
	}
	name := fmt.Sprintf("codego-%s-%d-%d", service, os.Getpid(), time.Now().UnixNano())
	active := filepath.Join(directory, name+".active.log")
	file, err := os.OpenFile(active, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, nil, fmt.Errorf("open service log: %w", err)
	}
	closeLog := func() error {
		if err := file.Close(); err != nil {
			return err
		}
		return os.Rename(active, filepath.Join(directory, name+".log"))
	}
	return slog.New(slog.NewJSONHandler(io.MultiWriter(os.Stderr, file), nil)), closeLog, nil
}
