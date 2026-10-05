package main

import "testing"

func batchOf(t *testing.T, spans ...Span) *Batch {
	t.Helper()
	b, err := AnalyzeBatch(spans)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func findDiff(t *testing.T, diffs []OpDiff, svc, op string) OpDiff {
	t.Helper()
	for _, d := range diffs {
		if d.Service == svc && d.Operation == op {
			return d
		}
	}
	t.Fatalf("diff for %s/%s not found", svc, op)
	return OpDiff{}
}

func TestCompareCommonAppearedDisappeared(t *testing.T) {
	base := batchOf(t,
		sp("t1", "a", "", "gw", "handle", 0, 100),
		sp("t1", "b", "a", "svc", "old", 0, 50),
	)
	next := batchOf(t,
		sp("t2", "a", "", "gw", "handle", 0, 200),
		sp("t2", "c", "a", "svc", "new", 0, 50),
	)
	diffs := CompareBatches(base, next)

	common := findDiff(t, diffs, "gw", "handle")
	if common.Change != ChangeCommon {
		t.Fatalf("change = %s, want common", common.Change)
	}
	if common.BaseAvgExclusive != 50 || common.NewAvgExclusive != 150 {
		t.Fatalf("avg exclusive = %v -> %v, want 50 -> 150", common.BaseAvgExclusive, common.NewAvgExclusive)
	}
	if common.AvgExclusiveDelta != 100 {
		t.Fatalf("delta = %v, want 100", common.AvgExclusiveDelta)
	}
	if common.CallsDelta != 0 {
		t.Fatalf("calls delta = %d, want 0", common.CallsDelta)
	}

	appeared := findDiff(t, diffs, "svc", "new")
	if appeared.Change != ChangeAppeared || appeared.BaseCalls != 0 || appeared.NewCalls != 1 {
		t.Fatalf("bad appeared diff: %+v", appeared)
	}
	gone := findDiff(t, diffs, "svc", "old")
	if gone.Change != ChangeDisappeared || gone.BaseCalls != 1 || gone.NewCalls != 0 {
		t.Fatalf("bad disappeared diff: %+v", gone)
	}
	// Deterministic order: sorted by service then operation.
	want := []string{"gw/handle", "svc/new", "svc/old"}
	for i, w := range want {
		got := diffs[i].Service + "/" + diffs[i].Operation
		if got != w {
			t.Fatalf("diffs[%d] = %s, want %s", i, got, w)
		}
	}
}

func TestCompareCallVolumeChange(t *testing.T) {
	base := batchOf(t, sp("t1", "a", "", "svc", "op", 0, 10))
	next := batchOf(t,
		sp("t2", "a", "", "svc", "op", 0, 10),
		sp("t3", "b", "", "svc", "op", 0, 30),
	)
	d := findDiff(t, CompareBatches(base, next), "svc", "op")
	if d.CallsDelta != 1 {
		t.Fatalf("calls delta = %d, want 1", d.CallsDelta)
	}
	if d.NewAvgExclusive != 20 {
		t.Fatalf("new avg exclusive = %v, want 20", d.NewAvgExclusive)
	}
}
