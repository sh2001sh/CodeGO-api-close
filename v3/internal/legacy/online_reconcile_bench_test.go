package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Isolate the real comparator's CPU work from PostgreSQL and network latency.
// The synthetic responder preserves one result per submitted projection.
type retainedBenchmarkTx struct{ pgx.Tx }

func (tx retainedBenchmarkTx) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	return &retainedBenchmarkBatch{tx: tx, ctx: ctx, batch: batch}
}

type retainedBenchmarkBatch struct {
	pgx.BatchResults
	tx    retainedBenchmarkTx
	ctx   context.Context
	batch *pgx.Batch
	next  int
}

func (b *retainedBenchmarkBatch) Query() (pgx.Rows, error) {
	if b.next >= b.batch.Len() {
		return nil, errors.New("unexpected benchmark batch result")
	}
	query := b.batch.QueuedQueries[b.next]
	b.next++
	return b.tx.Query(b.ctx, query.SQL, query.Arguments...)
}

func (b *retainedBenchmarkBatch) Close() (err error) {
	for b.next < b.batch.Len() {
		rows, queryErr := b.Query()
		if queryErr != nil {
			err = errors.Join(err, queryErr)
			continue
		}
		rows.Close()
		err = errors.Join(err, rows.Err())
	}
	return err
}

func (retainedBenchmarkTx) Query(_ context.Context, query string, args ...any) (pgx.Rows, error) {
	if strings.HasPrefix(query, "SELECT r.row_key,r.metrics") {
		return &retainedBenchmarkRows{}, nil
	}
	if !strings.HasPrefix(query, "SELECT EXISTS(") || len(args) != 1 {
		return nil, fmt.Errorf("unexpected benchmark query")
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(args[0].([]byte), &rows); err != nil {
		return nil, err
	}
	return &retainedBenchmarkRows{remaining: len(rows)}, nil
}

type retainedBenchmarkRows struct {
	pgx.Rows
	remaining int
}

func (r *retainedBenchmarkRows) Next() bool {
	if r.remaining == 0 {
		return false
	}
	r.remaining--
	return true
}
func (*retainedBenchmarkRows) Scan(dest ...any) error { *dest[0].(*bool) = true; return nil }
func (*retainedBenchmarkRows) Close()                 {}
func (*retainedBenchmarkRows) Err() error             { return nil }

func BenchmarkOnlineRetainedLogComparisonCPU(b *testing.B) {
	p := &onlineProjector{target: retainedBenchmarkTx{}, users: map[int64]bool{7: true},
		mappings: map[fundingAccountKey]int64{{"user", "wallet", 7}: 17}, promptAnomalySchema: true}
	spec := onlineSpec{name: "logs"}
	for _, size := range []int{2048, 8192} {
		inputs := make([]onlineInput, 512)
		for i := range inputs {
			other, _ := json.Marshal(map[string]any{"cached_tokens": 23, "precise": int64(9007199254740993), "context": strings.Repeat("x", size)})
			raw, _ := json.Marshal(map[string]any{"id": i + 1, "user_id": 7, "type": 2,
				"created_at": 1700000000 + i, "quota": 17, "prompt_tokens": 23, "completion_tokens": 41,
				"content": "synthetic", "request_id": fmt.Sprintf("synthetic-%d", i), "other": string(other)})
			inputs[i] = onlineInput{json.RawMessage(fmt.Sprintf(`{"id":%d}`, i+1)), raw}
		}
		b.Run(fmt.Sprintf("%dB", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(inputs) * size))
			for i := 0; i < b.N; i++ {
				matches, err := p.matchRetainedInputs(context.Background(), spec, inputs)
				if err != nil || len(matches) != len(inputs) {
					b.Fatalf("comparison returned %d rows: %v", len(matches), err)
				}
			}
		})
	}
}
