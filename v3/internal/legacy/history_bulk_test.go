package legacy

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestHistoryBatchBoundsRowsAndBytes(t *testing.T) {
	var sizes []int
	b := historyBatch{flush: func(rows []map[string]any) error {
		sizes = append(sizes, len(rows))
		total := 0
		for _, row := range rows {
			encoded, err := json.Marshal(row)
			if err != nil {
				return err
			}
			total += len(encoded)
		}
		if len(rows) > exactBulkRows || total > exactBulkBytes && len(rows) != 1 {
			t.Fatalf("unbounded batch rows=%d bytes=%d", len(rows), total)
		}
		return nil
	}}
	medium := strings.Repeat("m", exactBulkBytes/2+1)
	large := strings.Repeat("l", exactBulkBytes+1)
	for _, value := range []string{medium, medium, large, "small", "tail"} {
		if err := b.add(map[string]any{"metadata": value}); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.finish(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sizes, []int{1, 1, 1, 2}) || b.bytes != 0 {
		t.Fatalf("byte batch sizes=%v bytes=%d", sizes, b.bytes)
	}
	sizes = nil
	for i := 0; i < exactBulkRows+1; i++ {
		if err := b.add(map[string]any{"id": i}); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.add(map[string]any{"metadata": json.RawMessage(`{invalid`)}); err == nil {
		t.Fatal("invalid JSON projection silently buffered")
	}
	if err := b.finish(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sizes, []int{exactBulkRows, 1}) {
		t.Fatalf("row batch sizes=%v", sizes)
	}
}
