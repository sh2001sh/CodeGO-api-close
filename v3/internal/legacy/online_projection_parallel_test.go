package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func parallelProjectionInputs(b testing.TB, bytes int) (*onlineProjector, []onlineInput) {
	b.Helper()
	p := &onlineProjector{users: map[int64]bool{7: true}, mappings: map[fundingAccountKey]int64{{"user", "wallet", 7}: 901}}
	inputs := make([]onlineInput, 4096)
	for i := range inputs {
		raw, err := json.Marshal(map[string]any{"id": i + 1, "user_id": 7, "created_at": 1700300000 + i, "type": 2, "content": strings.Repeat("x", bytes), "model_name": "model", "quota": 1, "prompt_tokens": 1, "completion_tokens": 1, "channel_id": 13, "token_id": 11, "request_id": fmt.Sprintf("parallel-%d", i), "other": `{"large_integer":9007199254740993}`})
		if err != nil {
			b.Fatal(err)
		}
		inputs[i] = onlineInput{raw: raw}
	}
	return p, inputs
}

func BenchmarkOnlineProjectionParallel(b *testing.B) {
	for _, bytes := range []int{128, 2048} {
		for _, workers := range []int{1, 2} {
			b.Run(fmt.Sprintf("bytes=%d/workers=%d", bytes, workers), func(b *testing.B) {
				previous := runtime.GOMAXPROCS(workers)
				defer runtime.GOMAXPROCS(previous)
				p, inputs := parallelProjectionInputs(b, bytes)
				pageBytes := 0
				for i, input := range inputs {
					if pageBytes+len(input.raw) > exactBulkBytes {
						inputs = inputs[:i]
						break
					}
					pageBytes += len(input.raw)
				}
				b.ReportAllocs()
				b.ReportMetric(float64(len(inputs)), "rows/op")
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					out, err := p.projectBatch(context.Background(), onlineSpec{name: "logs"}, inputs, nil)
					if err != nil || len(out) != len(inputs) || len(out[len(out)-1]) != 2 {
						b.Fatalf("trial projection output differs: %v", err)
					}
				}
			})
		}
	}
}

func TestOnlineProjectionParallelExactOrderAndImmutableDependencies(t *testing.T) {
	previous := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(previous)
	p, inputs := parallelProjectionInputs(t, 2048)
	spec := onlineSpec{name: "logs"}
	want := make([][]onlineProjection, len(inputs))
	for i, input := range inputs {
		var err error
		want[i], err = p.project(context.Background(), spec, input.raw, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			for _, n := range []int{0, 1, 511, 512, 4096} {
				out, err := p.projectBatch(context.Background(), spec, inputs[:n], nil)
				if err != nil || !reflect.DeepEqual(out, want[:n]) {
					t.Errorf("ordered exact output differs at boundary %d: %v", n, err)
				}
			}
		})
	}
	wg.Wait()
	if len(p.users) != 1 || !p.users[7] || len(p.mappings) != 1 || p.mappings[fundingAccountKey{"user", "wallet", 7}] != 901 {
		t.Fatal("projection changed shared dependency maps")
	}
}

func TestOnlineProjectionParallelFirstErrorCancellationAndDeletedInput(t *testing.T) {
	previous := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(previous)
	p, inputs := parallelProjectionInputs(t, 128)
	spec := onlineSpec{name: "logs"}
	inputs[100].raw = nil
	out, err := p.projectBatch(context.Background(), spec, inputs, nil)
	if err != nil || out[100] != nil || len(out[101]) != 2 {
		t.Fatalf("deleted input changed adjacent projection: %v", err)
	}
	inputs[100].raw = json.RawMessage("earlier-invalid-json")
	inputs[3000].raw = json.RawMessage("later-invalid-json")
	_, wantErr := p.project(context.Background(), spec, inputs[100].raw, nil)
	for range 10 {
		out, err = p.projectBatch(context.Background(), spec, inputs, nil)
		if out != nil || err == nil || err.Error() != wantErr.Error() {
			t.Fatalf("first input error changed: %v, want %v", err, wantErr)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if out, err = p.projectBatch(ctx, spec, inputs, nil); out != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled projection did not stop: %v", err)
	}
}
