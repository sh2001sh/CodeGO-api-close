package catalog

import (
	"reflect"
	"testing"
)

func TestAllowedAutoGroupsPreservesAuthorizationAndOrder(t *testing.T) {
	configured := map[string]struct{}{"z-public": {}, "a-private": {}, "blocked": {}}
	got := allowedAutoGroups([]string{"a-private", "other", "z-public"}, configured)
	if want := []string{"a-private", "z-public"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("automatic groups = %v, want %v", got, want)
	}
	for _, allowed := range [][]string{nil, {"other"}} {
		if got := allowedAutoGroups(allowed, configured); got == nil || len(got) != 0 {
			t.Fatalf("unauthorized groups leaked or empty SQL array changed: %#v", got)
		}
	}
	if got := allowedAutoGroups([]string{"default"}, nil); got == nil || len(got) != 0 {
		t.Fatalf("empty configured automatic groups = %#v", got)
	}
}
