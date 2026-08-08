package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUserLockManagerSerializesSameUser(t *testing.T) {
	manager := newUserLockManager()
	releaseFirst := manager.Lock(1)
	acquired := make(chan struct{})
	done := make(chan struct{})

	go func() {
		releaseSecond := manager.Lock(1)
		close(acquired)
		releaseSecond()
		close(done)
	}()

	select {
	case <-acquired:
		t.Fatal("same user lock must wait for the active request")
	case <-time.After(25 * time.Millisecond):
	}

	releaseFirst()
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 5*time.Millisecond)
	require.Empty(t, manager.entries)
}

func TestUserLockManagerAllowsDifferentUsers(t *testing.T) {
	manager := newUserLockManager()
	releaseFirst := manager.Lock(1)
	defer releaseFirst()

	done := make(chan struct{})
	go func() {
		releaseSecond := manager.Lock(2)
		releaseSecond()
		close(done)
	}()

	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 5*time.Millisecond)
}
