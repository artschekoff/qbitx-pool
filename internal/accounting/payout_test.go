package accounting

import "testing"

func TestPayoutAddressFromWorker(t *testing.T) {
	if got := payoutAddressFromWorker("addr123.worker01"); got != "addr123" {
		t.Fatalf("unexpected parsed worker: %s", got)
	}
	if got := payoutAddressFromWorker("  addrOnly  "); got != "addrOnly" {
		t.Fatalf("unexpected parsed plain worker: %s", got)
	}
}

func TestAllocateByWeightPreservesTotal(t *testing.T) {
	weights := map[string]float64{
		"a": 1,
		"b": 2,
		"c": 3,
	}
	out := allocateByWeight(100, weights)
	var sum int64
	for _, v := range out {
		sum += v
	}
	if sum != 100 {
		t.Fatalf("expected sum 100, got %d", sum)
	}
	if !(out["c"] >= out["b"] && out["b"] >= out["a"]) {
		t.Fatalf("unexpected proportional order: %#v", out)
	}
}
