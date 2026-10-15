package catalog

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Exercise the real pgx pool and PostgreSQL wire protocol without requiring
// a database. The server deliberately never grants the publication lock.
func TestPublishLockContentionReturnsPoolConnectionBeforeRetry(t *testing.T) {
	pool, attempted := publicationLockServer(t, false)
	p := &Publisher{pool: pool}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := p.acquirePublishLock(ctx)
		done <- err
	}()
	select {
	case <-attempted:
	case err := <-done:
		t.Fatalf("publisher failed before lock attempt: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("publisher did not try the advisory lock")
	}
	// With a one-connection pool this succeeds only if the waiting publisher
	// returned its connection. The old blocking advisory lock starved it.
	otherCtx, otherCancel := context.WithTimeout(context.Background(), coalesceWindow/2)
	defer otherCancel()
	conn, err := pool.Acquire(otherCtx)
	if err != nil {
		t.Fatalf("publisher monopolized pool while waiting for lock: %v", err)
	}
	defer conn.Release()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("lock wait did not honor cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("lock wait failed to stop on cancellation")
	}
}

func TestPublishLockQueryFailureReturnsPoolConnection(t *testing.T) {
	pool, _ := publicationLockServer(t, true)
	p := &Publisher{pool: pool}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := p.acquirePublishLock(ctx); err == nil {
		t.Fatal("failed advisory-lock query was accepted")
	}
	// pgxpool destroys closed resources asynchronously after Release.
	deadline := time.Now().Add(time.Second)
	for pool.Stat().AcquiredConns() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("failed lock query leaked %d connections", pool.Stat().AcquiredConns())
		}
		time.Sleep(time.Millisecond)
	}
}

func publicationLockServer(t *testing.T, fail bool) (*pgxpool.Pool, <-chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	attempted := make(chan struct{}, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		backend := pgproto3.NewBackend(conn, conn)
		if _, err := backend.ReceiveStartupMessage(); err != nil {
			return
		}
		backend.Send(&pgproto3.AuthenticationOk{})
		backend.Send(&pgproto3.ParameterStatus{Name: "server_version", Value: "17.0"})
		backend.Send(&pgproto3.ParameterStatus{Name: "standard_conforming_strings", Value: "on"})
		backend.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
		backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
		if err := backend.Flush(); err != nil {
			return
		}
		for {
			msg, err := backend.Receive()
			if err != nil {
				return
			}
			switch msg.(type) {
			case *pgproto3.Terminate:
				return
			case *pgproto3.Query:
				if fail {
					backend.Send(&pgproto3.ErrorResponse{Severity: "ERROR", Code: "55000", Message: "lock unavailable"})
				} else {
					backend.Send(&pgproto3.RowDescription{Fields: []pgproto3.FieldDescription{{Name: []byte("pg_try_advisory_lock"), DataTypeOID: 16, DataTypeSize: 1}}})
					backend.Send(&pgproto3.DataRow{Values: [][]byte{[]byte("f")}})
					backend.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
				}
				backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
				if err := backend.Flush(); err != nil {
					return
				}
				select {
				case attempted <- struct{}{}:
				default:
				}
			}
		}
	}()
	cfg, err := pgxpool.ParseConfig("postgres://test@" + listener.Addr().String() + "/test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool, attempted
}
