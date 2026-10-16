package cachex

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

// A real RESP connection verifies command count, missing values, decoding,
// and timeout behavior without depending on a production Redis instance.
func testReadCache(tb testing.TB, delay time.Duration) (*HybridCache[int], *atomic.Int64) {
	tb.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(tb, err)
	var calls atomic.Int64
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				reader := bufio.NewReader(conn)
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					n, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "*")))
					args := make([]string, n)
					for i := range args {
						line, err = reader.ReadString('\n')
						if err != nil {
							return
						}
						size, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "$")))
						data := make([]byte, size+2)
						if _, err := io.ReadFull(reader, data); err != nil {
							return
						}
						args[i] = string(data[:size])
					}
					calls.Add(1)
					time.Sleep(delay)
					if strings.EqualFold(args[0], "mget") {
						fmt.Fprintf(conn, "*%d\r\n", len(args)-1)
					}
					for _, key := range args[1:] {
						if strings.HasSuffix(key, "missing") {
							fmt.Fprint(conn, "$-1\r\n")
						} else if strings.HasSuffix(key, "invalid") {
							fmt.Fprint(conn, "$3\r\nbad\r\n")
						} else {
							fmt.Fprint(conn, "$2\r\n42\r\n")
						}
					}
				}
			}()
		}
	}()
	client := redis.NewClient(&redis.Options{Addr: listener.Addr().String(), MaxRetries: -1})
	tb.Cleanup(func() { client.Close(); listener.Close() })
	return NewHybridCache[int](HybridCacheConfig[int]{Namespace: Namespace("test"), Redis: client, RedisCodec: IntCodec{}}), &calls
}

func TestGetManyReadsOnceAndOmitsMissingKeys(t *testing.T) {
	cache, calls := testReadCache(t, 0)
	values, err := cache.GetManyWithTimeout([]string{"one", "two", "missing"}, time.Second)
	require.NoError(t, err)
	require.Equal(t, map[string]int{"one": 42, "two": 42}, values)
	require.EqualValues(t, 1, calls.Load())
	values, err = cache.GetManyWithTimeout(nil, time.Second)
	require.NoError(t, err)
	require.Empty(t, values)
	require.EqualValues(t, 1, calls.Load())
}

func TestGetManyReturnsDecodeError(t *testing.T) {
	cache, _ := testReadCache(t, 0)
	values, err := cache.GetManyWithTimeout([]string{"one", "invalid"}, time.Second)
	require.Error(t, err)
	require.Equal(t, map[string]int{"one": 42}, values, "one invalid entry must not discard other channels' health")
}

func TestGetManyUsesOneTimeoutBudget(t *testing.T) {
	cache, _ := testReadCache(t, 200*time.Millisecond)
	start := time.Now()
	_, err := cache.GetManyWithTimeout([]string{"one", "two", "three"}, 20*time.Millisecond)
	require.Error(t, err)
	require.Less(t, time.Since(start), 150*time.Millisecond)
}

func BenchmarkHealthReads(b *testing.B) {
	for _, batch := range []bool{false, true} {
		b.Run(fmt.Sprintf("batch=%v/members=32", batch), func(b *testing.B) {
			cache, _ := testReadCache(b, time.Millisecond)
			keys := make([]string, 32)
			for i := range keys {
				keys[i] = strconv.Itoa(i)
			}
			b.ResetTimer()
			for b.Loop() {
				if batch {
					_, err := cache.GetManyWithTimeout(keys, time.Second)
					require.NoError(b, err)
				} else {
					for _, key := range keys {
						_, _, err := cache.GetWithTimeout(key, time.Second)
						require.NoError(b, err)
					}
				}
			}
		})
	}
}
