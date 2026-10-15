package httpx

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemoryBodyStorageNewReaderUsesIndependentCursor(t *testing.T) {
	storage := newMemoryStorage([]byte("replay-body"))
	t.Cleanup(func() { _ = storage.Close() })

	prefix := make([]byte, 6)
	_, err := io.ReadFull(storage, prefix)
	require.NoError(t, err)
	require.Equal(t, "replay", string(prefix))

	replay, err := storage.NewReader()
	require.NoError(t, err)
	t.Cleanup(func() { _ = replay.Close() })
	replayed, err := io.ReadAll(replay)
	require.NoError(t, err)
	require.Equal(t, "replay-body", string(replayed))

	remainder, err := io.ReadAll(storage)
	require.NoError(t, err)
	require.Equal(t, "-body", string(remainder))
}

func TestDiskBodyStorageNewReaderUsesIndependentFile(t *testing.T) {
	storage, err := newDiskStorage([]byte("disk-replay"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })

	prefix := make([]byte, 4)
	_, err = io.ReadFull(storage, prefix)
	require.NoError(t, err)

	replay, err := storage.NewReader()
	require.NoError(t, err)
	t.Cleanup(func() { _ = replay.Close() })
	replayed, err := io.ReadAll(replay)
	require.NoError(t, err)
	require.Equal(t, "disk-replay", string(replayed))

	remainder, err := io.ReadAll(storage)
	require.NoError(t, err)
	require.Equal(t, "-replay", string(remainder))
}
