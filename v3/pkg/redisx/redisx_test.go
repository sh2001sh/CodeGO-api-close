package redisx

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCommandReadHonorsContextDeadlineAfterAuthenticatedConnect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	authenticated := make(chan struct{}, 1)
	blackholed := make(chan struct{})
	serverErrors := make(chan error, 1)
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			serverErrors <- err
			return
		}
		defer func() { _ = conn.Close() }()
		reader := bufio.NewReader(conn)
		for {
			command, err := readRESPCommand(reader)
			if err != nil {
				if !errors.Is(err, io.EOF) {
					serverErrors <- err
				}
				return
			}
			var reply string
			switch strings.ToUpper(command[0]) {
			case "HELLO":
				// Exercise the supported RESP2 fallback and its real AUTH round trip.
				reply = "-ERR unknown command 'hello'\r\n"
			case "AUTH":
				if len(command) != 2 || command[1] != "local-test-password" {
					reply = "-WRONGPASS invalid password\r\n"
				} else {
					authenticated <- struct{}{}
					reply = "+OK\r\n"
				}
			case "CLIENT", "SELECT":
				reply = "+OK\r\n"
			case "PING":
				reply = "+PONG\r\n"
			case "GET":
				if len(command) == 2 && command[1] == "blackhole" {
					close(blackholed)
					// Keep the authenticated TCP connection open without a command reply.
					<-stop
					return
				}
				reply = "$2\r\nok\r\n"
			default:
				reply = "-ERR unexpected command\r\n"
			}
			if _, err := io.WriteString(conn, reply); err != nil {
				serverErrors <- err
				return
			}
		}
	}()
	t.Cleanup(func() {
		close(stop)
		_ = ln.Close()
		<-done
		select {
		case err := <-serverErrors:
			t.Errorf("local RESP server: %v", err)
		default:
		}
	})

	client, err := Connect(Config{Addr: ln.Addr().String(), Password: "local-test-password", PoolSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	select {
	case <-authenticated:
	default:
		t.Fatal("Connect completed without the AUTH round trip")
	}
	if value, err := client.Get(context.Background(), "ready").Result(); err != nil || value != "ok" {
		t.Fatalf("normal command = %q, %v", value, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = client.Get(ctx, "blackhole").Result()
	elapsed := time.Since(started)
	if err == nil || ctx.Err() != context.DeadlineExceeded {
		t.Fatalf("blackholed command = %v, context = %v", err, ctx.Err())
	}
	select {
	case <-blackholed:
	default:
		t.Fatal("deadline fired before the server received the command")
	}
	if elapsed > time.Second {
		t.Fatalf("blackholed command took %v; want the short context deadline, below the 3s read timeout", elapsed)
	}
	t.Logf("authenticated command read returned after %v with %v", elapsed, err)
}

func readRESPCommand(reader *bufio.Reader) ([]string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(line, "*") {
		return nil, fmt.Errorf("expected RESP command array")
	}
	count, err := strconv.Atoi(strings.TrimSpace(line[1:]))
	if err != nil || count <= 0 {
		return nil, fmt.Errorf("invalid RESP command length")
	}
	command := make([]string, count)
	for index := range command {
		line, err = reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(line, "$") {
			return nil, fmt.Errorf("expected RESP bulk argument")
		}
		length, err := strconv.Atoi(strings.TrimSpace(line[1:]))
		if err != nil || length < 0 {
			return nil, fmt.Errorf("invalid RESP argument length")
		}
		argument := make([]byte, length+2)
		if _, err := io.ReadFull(reader, argument); err != nil {
			return nil, err
		}
		command[index] = string(argument[:length])
	}
	return command, nil
}

// Connect pings before returning, so an unreachable Redis is reported at
// start instead of on the first request.
func TestConnectFailsFastWhenUnreachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // nothing listens here now

	start := time.Now()
	c, err := Connect(Config{Addr: addr})
	if err == nil {
		_ = c.Close()
		t.Fatal("Connect to a closed port succeeded")
	}
	if !strings.Contains(err.Error(), "ping") {
		t.Errorf("error = %v; want the ping failure", err)
	}
	if d := time.Since(start); d > 6*time.Second {
		t.Errorf("Connect took %v; want it bounded by the 5s ping timeout", d)
	}
}
