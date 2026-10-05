package trace

import "testing"

func summarizeLines(t *testing.T, lines ...string) Summary {
	t.Helper()
	spans := mustParse(t, lines...)
	traces, anomalies, invalid := Build(spans)
	return Summarize(traces, anomalies, invalid)
}

// Cross-batch comparison: matched ops report avg-exclusive and call deltas;
// new/removed ops are listed separately and never treated as zero-time.
func TestCompareBatches(t *testing.T) {
	a := summarizeLines(t,
		`{"trace_id":"t1","span_id":"1","service":"api","operation":"get","start_us":0,"end_us":10}`,
		`{"trace_id":"t1","span_id":"2","service":"api","operation":"get","start_us":20,"end_us":40}`,
		`{"trace_id":"t1","span_id":"3","service":"db","operation":"q","start_us":0,"end_us":5}`,
	)
	b := summarizeLines(t,
		`{"trace_id":"t2","span_id":"1","service":"api","operation":"get","start_us":0,"end_us":30}`,
		`{"trace_id":"t2","span_id":"2","service":"cache","operation":"get","start_us":0,"end_us":7}`,
	)
	matched, added, removed := Compare(a, b)
	if len(matched) != 1 || len(added) != 1 || len(removed) != 1 {
		t.Fatalf("matched=%d added=%d removed=%d, want 1/1/1",
			len(matched), len(added), len(removed))
	}
	m := matched[0]
	if m.Service != "api" || m.Operation != "get" {
		t.Fatalf("unexpected matched op: %+v", m)
	}
	// A avg exclusive: (10+20)/2 = 15; B: 30. Delta = +15, calls delta = -1.
	if m.AvgExclusiveA != 15 || m.AvgExclusiveB != 30 || m.AvgExclusiveDelta != 15 {
		t.Errorf("avg exclusive mismatch: %+v", m)
	}
	if m.CallsA != 2 || m.CallsB != 1 || m.CallsDelta != -1 {
		t.Errorf("calls mismatch: %+v", m)
	}
	if added[0].Service != "cache" || added[0].Status != "new" {
		t.Errorf("unexpected added: %+v", added[0])
	}
	if removed[0].Service != "db" || removed[0].Status != "removed" {
		t.Errorf("unexpected removed: %+v", removed[0])
	}
	// New/removed entries must not carry a fabricated zero counterpart delta.
	if added[0].AvgExclusiveDelta != 0 || removed[0].AvgExclusiveDelta != 0 {
		t.Error("single-sided ops must not report an avg-exclusive delta")
	}
}

// Aggregation sanity: inclusive/exclusive sums per service+operation and
// fixed sort order.
func TestSummarizeAggregation(t *testing.T) {
	sum := summarizeLines(t,
		`{"trace_id":"t1","span_id":"r","service":"api","operation":"h","start_us":0,"end_us":10}`,
		`{"trace_id":"t1","span_id":"c","parent_id":"r","service":"db","operation":"q","start_us":2,"end_us":6}`,
		`{"trace_id":"t2","span_id":"r","service":"api","operation":"h","start_us":0,"end_us":4}`,
	)
	if sum.ValidTraces != 2 || sum.TotalSpans != 3 {
		t.Errorf("summary counts: %+v", sum)
	}
	// active: t1 [0,10) = 10, t2 [0,4) = 4 -> 14.
	if sum.TotalActiveUs != 14 {
		t.Errorf("total active = %d, want 14", sum.TotalActiveUs)
	}
	if len(sum.Operations) != 2 {
		t.Fatalf("operations: %+v", sum.Operations)
	}
	// sorted by (service, operation): api/h before db/q.
	if sum.Operations[0].Service != "api" || sum.Operations[1].Service != "db" {
		t.Errorf("sort order wrong: %+v", sum.Operations)
	}
	api := sum.Operations[0]
	if api.Calls != 2 || api.InclusiveUs != 14 || api.ExclusiveUs != 10 {
		t.Errorf("api stats wrong: %+v", api)
	}
	db := sum.Operations[1]
	if db.Calls != 1 || db.InclusiveUs != 4 || db.ExclusiveUs != 4 {
		t.Errorf("db stats wrong: %+v", db)
	}
}
