package legacy

import (
	"fmt"
	"math"
	"reflect"
	"testing"
)

func TestPendingOpeningIncludesMissingAndRemovesReleasedIncome(t *testing.T) {
	want := map[string]sampledPendingIncome{"same": {7, 10}, "missing": {7, 20}, "changed": {8, 30}}
	actual := map[string]sampledPendingIncome{"same": {7, 10}, "released": {7, 100}, "changed": {8, 40}}
	ids, sums, err := sampledPendingDifference(want, actual)
	if err != nil || !reflect.DeepEqual(ids, []string{"changed", "missing", "released"}) || sums[7] != 30 || sums[8] != 30 {
		t.Fatalf("incorrect current obligation repair: %v %v %v", ids, sums, err)
	}
	if _, _, err = sampledPendingDifference(map[string]sampledPendingIncome{"a": {7, math.MaxInt64}, "b": {7, 1}}, nil); err == nil {
		t.Fatal("overflow accepted")
	}
	large := map[string]sampledPendingIncome{}
	for i := 0; i < 1025; i++ {
		large[fmt.Sprint(i)] = sampledPendingIncome{7, 1}
	}
	if _, _, err = sampledPendingDifference(large, nil); err == nil {
		t.Fatal("unbounded repair accepted")
	}
}
