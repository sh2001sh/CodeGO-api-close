package legacy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestExactBulkCachedEncodingPreservesBytesRawJSONAndShape(t *testing.T) {
	rows := []map[string]any{
		{"id": int64(9007199254740993), "amount": int64(-9223372036854775807), "data": []byte{0, 255}, "metadata": json.RawMessage(`{"precise":9223372036854775807,"html":"<"}`)},
		{"id": int64(2), "amount": int64(0), "data": []byte{}, "metadata": json.RawMessage(`null`)},
		{"id": int64(3), "amount": int64(1), "data": []byte(nil), "metadata": json.RawMessage(`{}`)},
	}
	encoded := make([][]byte, len(rows))
	for i, row := range rows {
		var err error
		encoded[i], err = exactBulkEncodeRow(row)
		if err != nil {
			t.Fatal(err)
		}
	}
	name, columns, ordinary, err := exactBulkInput("v3_audit", "test", []string{"id"}, rows)
	if err != nil {
		t.Fatal(err)
	}
	cachedName, cachedColumns, cached, err := exactBulkEncodedInput("v3_audit", "test", []string{"id"}, rows, encoded)
	if err != nil || name != cachedName || !reflect.DeepEqual(columns, cachedColumns) || !bytes.Equal(ordinary, cached) {
		t.Fatalf("cached and ordinary differ: err=%v cached=%s ordinary=%s", err, cached, ordinary)
	}
	if !bytes.Equal(rows[0]["data"].([]byte), []byte{0, 255}) || string(rows[0]["metadata"].(json.RawMessage)) != `{"precise":9223372036854775807,"html":"<"}` {
		t.Fatal("normalization mutated input")
	}
	for _, tc := range []struct {
		rows    []map[string]any
		encoded [][]byte
	}{
		{rows, encoded[:2]}, {rows, [][]byte{encoded[0], nil, encoded[2]}},
		{[]map[string]any{{"id": nil}}, [][]byte{[]byte(`{"id":null}`)}},
		{[]map[string]any{{"id": 1}, {"other": 2}}, [][]byte{[]byte(`{"id":1}`), []byte(`{"other":2}`)}},
	} {
		if _, _, _, err := exactBulkEncodedInput("v3_audit", "test", []string{"id"}, tc.rows, tc.encoded); err == nil {
			t.Fatal("invalid cached batch accepted")
		}
	}
	if _, err := exactBulkEncodeRow(map[string]any{"metadata": json.RawMessage(`{bad`)}); err == nil {
		t.Fatal("invalid raw JSON accepted")
	}
}

func TestHistoryCachedBatchFlushFailureKeepsBoundedProjection(t *testing.T) {
	want := errors.New("flush failed")
	calls := 0
	b := historyBatch{flushEncoded: func(rows []map[string]any, encoded [][]byte) error {
		calls++
		if len(rows) != 1 || len(encoded) != 1 {
			t.Fatal("lost cached projection")
		}
		return want
	}}
	if err := b.add(map[string]any{"id": 1}); err != nil {
		t.Fatal(err)
	}
	if err := b.finish(); !errors.Is(err, want) || calls != 1 || len(b.rows) != 1 || len(b.encoded) != 1 {
		t.Fatalf("flush failure lost: %v calls=%d", err, calls)
	}
}

func TestHistoryCachedBatchPreservesOversizedOfflineRow(t *testing.T) {
	// Offline history has always allowed a single large row in its own batch.
	// Online's 64 MiB source/projection limit must not alter that compatibility.
	payload := strings.Repeat("x", onlineRowMaxBytes) + "<"
	var ids []int
	b := historyBatch{flushEncoded: func(rows []map[string]any, encoded [][]byte) error {
		if len(rows) != 1 || len(encoded) != 1 {
			t.Fatalf("oversized row shared a batch: rows=%d encoded=%d", len(rows), len(encoded))
		}
		id := rows[0]["id"].(int)
		ids = append(ids, id)
		if id != 2 {
			return nil
		}
		if len(encoded[0]) <= onlineRowMaxBytes || bytes.Count(encoded[0], []byte("x")) != onlineRowMaxBytes {
			t.Fatal("oversized historical payload was shortened")
		}
		_, _, data, err := exactBulkEncodedInput("v3_audit", "test", []string{"id"}, rows, encoded)
		if err != nil {
			return err
		}
		if len(data) != len(encoded[0])+2 || data[0] != '[' || data[len(data)-1] != ']' || !bytes.Equal(data[1:len(data)-1], encoded[0]) || !bytes.HasSuffix(data, []byte(`\u003c"}]`)) {
			t.Fatal("oversized cached JSON changed while joining the array")
		}
		return nil
	}}
	for _, row := range []map[string]any{{"id": 1, "metadata": "before"}, {"id": 2, "metadata": payload}, {"id": 3, "metadata": "after"}} {
		if err := b.add(row); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.finish(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []int{1, 2, 3}) || len(b.rows) != 0 || len(b.encoded) != 0 || b.bytes != 0 {
		t.Fatalf("offline batch order/state changed ids=%v", ids)
	}
}

func TestOnlineMetricFieldsMatchFullJSONExactly(t *testing.T) {
	for _, status := range []any{"pending", "paid", json.RawMessage(`"pending"`)} {
		fields := map[string]any{"amount": int64(-9223372036854775807), "original_amount": json.RawMessage(`9007199254740993`), "remaining_amount": nil, "actual_amount": json.RawMessage(`null`), "consumer_micro": int64(3), "gross_micro": int64(2), "commission_micro": int64(1), "fee_micro": int64(0), "net_micro": int64(9007199254740993), "reclaimed_micro": int64(1), "owner_user_id": int64(9007199254740993), "status": status, "metadata": json.RawMessage(`{"amount":0.1,"nested":[{"large":"not a metric"}]}`)}
		raw, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		full, scalar := onlineMetrics{}, onlineMetrics{}
		for _, sign := range []int64{1, -1, 1} {
			if err := full.row("v3_channelmarket.settlements", raw, sign); err != nil {
				t.Fatal(err)
			}
			if err := scalar.fields("v3_channelmarket.settlements", fields, sign); err != nil {
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(full, scalar) {
			t.Fatalf("metric mismatch: full=%v scalar=%v", full, scalar)
		}
	}
	for _, invalid := range []any{0.5, json.RawMessage(`0.5`), "1"} {
		if err := (onlineMetrics{}).fields("v3_billing.historical_entries", map[string]any{"amount": invalid}, 1); err == nil {
			t.Fatalf("invalid integer accepted: %v", invalid)
		}
	}
}

// The baseline mirrors the former production pipeline: size every row, copy
// every map and encode the batch, then encode/decode rows again for metrics.
func baselineProjectionEncoding(rows []map[string]any, metrics bool) ([]byte, error) {
	for _, row := range rows {
		if _, err := json.Marshal(row); err != nil {
			return nil, err
		}
	}
	if _, _, err := exactBulkShape("v3_billing", "historical_entries", []string{"id"}, rows); err != nil {
		return nil, err
	}
	copies := make([]map[string]any, len(rows))
	for i, row := range rows {
		copies[i] = exactBulkByteaRow(row)
	}
	data, err := json.Marshal(copies)
	if err != nil {
		return nil, err
	}
	if metrics {
		m := onlineMetrics{}
		for _, row := range rows {
			raw, err := json.Marshal(row)
			if err != nil {
				return nil, err
			}
			if err := m.row("v3_billing.historical_entries", raw, 1); err != nil {
				return nil, err
			}
		}
	}
	return data, nil
}

var projectionBenchmarkSink []byte

func BenchmarkProjectionEncoding(b *testing.B) {
	for _, size := range []int{64, 2048, 65536} {
		count := exactBulkRows
		if size == 65536 {
			count = 48
		} // Stay inside the production 4 MiB batch cap.
		rows := make([]map[string]any, count)
		metadata := json.RawMessage(`{"context":"` + strings.Repeat("x", size) + `","precise":9007199254740993}`)
		for i := range rows {
			rows[i] = map[string]any{"id": int64(i), "account_id": int64(9007199254740993), "kind": "settle_debit", "direction": "debit", "amount": int64(9007199254740993), "balance_after": nil, "request_id": "request", "reference_type": "usage", "idempotency_key": fmt.Sprintf("key-%d", i), "metadata": metadata}
		}
		for _, metrics := range []bool{false, true} {
			pipeline := "history"
			if metrics {
				pipeline = "online"
			}
			for _, optimized := range []bool{false, true} {
				version := "baseline"
				if optimized {
					version = "optimized"
				}
				b.Run(fmt.Sprintf("%s/%dB/%s", pipeline, size, version), func(b *testing.B) {
					b.ReportAllocs()
					b.SetBytes(int64(count * size))
					for i := 0; i < b.N; i++ {
						var data []byte
						var err error
						if !optimized {
							data, err = baselineProjectionEncoding(rows, metrics)
						} else {
							encoded := make([][]byte, len(rows))
							for j, row := range rows {
								encoded[j], err = exactBulkEncodeRow(row)
								if err != nil {
									break
								}
							}
							if err == nil {
								_, _, data, err = exactBulkEncodedInput("v3_billing", "historical_entries", []string{"id"}, rows, encoded)
							}
							if err == nil && metrics {
								m := onlineMetrics{}
								for _, row := range rows {
									if err = m.fields("v3_billing.historical_entries", row, 1); err != nil {
										break
									}
								}
							}
						}
						if err != nil {
							b.Fatal(err)
						}
						projectionBenchmarkSink = data
					}
				})
			}
		}
	}
}
