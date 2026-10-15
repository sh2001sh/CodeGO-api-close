package concurrency

import "testing"

func TestRelayAdmissionRejectsWithoutQueueing(t *testing.T) {
	ConfigureRelayAdmission(1)
	t.Cleanup(func() { ConfigureRelayAdmission(0) })

	release, admitted, stats := TryAcquireRelaySlot()
	if !admitted || stats.Active != 1 {
		t.Fatalf("first relay should be admitted: admitted=%t stats=%+v", admitted, stats)
	}

	_, admitted, stats = TryAcquireRelaySlot()
	if admitted || stats.Active != 1 || stats.Rejected != 1 {
		t.Fatalf("second relay should be rejected immediately: admitted=%t stats=%+v", admitted, stats)
	}

	release()
	release()
	if stats = RelayAdmissionSnapshot(); stats.Active != 0 {
		t.Fatalf("release must be idempotent: stats=%+v", stats)
	}

	_, admitted, _ = TryAcquireRelaySlot()
	if !admitted {
		t.Fatal("slot should be available after release")
	}
}
