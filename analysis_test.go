package main

import (
	"strings"
	"testing"
)

func sp(trace, id, parent, svc, op string, start, end int64) Span {
	return Span{TraceID: trace, SpanID: id, ParentID: parent, Service: svc, Operation: op, StartUs: start, EndUs: end}
}

func find(t *testing.T, tr *Trace, id string) *Node {
	t.Helper()
	for _, n := range tr.All() {
		if n.Span.SpanID == id {
			return n
		}
	}
	t.Fatalf("span %s not found", id)
	return nil
}

// Parallel children overlap in time; their union must be subtracted once.
func TestExclusiveParallelChildren(t *testing.T) {
	tr, _ := BuildTrace([]Span{
		sp("t1", "root", "", "gw", "handle", 0, 100),
		sp("t1", "a", "root", "svc", "q", 10, 60),
		sp("t1", "b", "root", "svc", "q", 40, 90),
	})
	if got := find(t, tr, "root").Exclusive; got != 20 {
		t.Fatalf("root exclusive = %d, want 20", got)
	}
	if tr.ActiveUs != 100 {
		t.Fatalf("active = %d, want 100", tr.ActiveUs)
	}
}

// Grandchildren must not be subtracted from the grandparent.
func TestExclusiveIgnoresGrandchildren(t *testing.T) {
	tr, _ := BuildTrace([]Span{
		sp("t1", "root", "", "gw", "handle", 0, 100),
		sp("t1", "mid", "root", "svc", "q", 10, 90),
		sp("t1", "leaf", "mid", "db", "sql", 20, 80),
	})
	if got := find(t, tr, "root").Exclusive; got != 20 {
		t.Fatalf("root exclusive = %d, want 20", got)
	}
	if got := find(t, tr, "mid").Exclusive; got != 20 {
		t.Fatalf("mid exclusive = %d, want 20", got)
	}
}

// Child extending past the parent is clipped and reported.
func TestExclusiveClipsOutOfRangeChild(t *testing.T) {
	tr, anomalies := BuildTrace([]Span{
		sp("t1", "root", "", "gw", "handle", 0, 100),
		sp("t1", "a", "root", "svc", "q", 50, 200),
	})
	if got := find(t, tr, "root").Exclusive; got != 50 {
		t.Fatalf("root exclusive = %d, want 50", got)
	}
	found := false
	for _, a := range anomalies {
		if a.Kind == "child_out_of_range" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected child_out_of_range anomaly")
	}
}

// Missing parent keeps the orphan branch and marks the trace incomplete.
func TestMissingParentOrphan(t *testing.T) {
	tr, anomalies := BuildTrace([]Span{
		sp("t1", "root", "", "gw", "handle", 0, 100),
		sp("t1", "orphan", "ghost", "svc", "q", 10, 20),
	})
	if tr.Complete {
		t.Fatal("trace should be incomplete")
	}
	if !tr.Valid {
		t.Fatal("trace should still be valid")
	}
	if len(tr.Roots) != 2 {
		t.Fatalf("roots = %d, want 2", len(tr.Roots))
	}
	if find(t, tr, "orphan").Parent != nil {
		t.Fatal("orphan must not get a fabricated parent")
	}
	found := false
	for _, a := range anomalies {
		if a.Kind == "missing_parent" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected missing_parent anomaly")
	}
}

// A cyclic trace is invalid but does not stop other traces.
func TestCycleInvalidatesOnlyItsTrace(t *testing.T) {
	b, err := AnalyzeBatch([]Span{
		sp("bad", "x", "y", "svc", "q", 0, 10),
		sp("bad", "y", "x", "svc", "q", 0, 10),
		sp("good", "r", "", "svc", "q", 0, 10),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Traces) != 1 || b.Traces[0].ID != "good" {
		t.Fatalf("expected only trace good, got %+v", b.Traces)
	}
	found := false
	for _, a := range b.Anomalies {
		if a.Kind == "invalid_trace" && a.TraceID == "bad" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected invalid_trace anomaly for bad")
	}
}

func TestSelfReferenceInvalid(t *testing.T) {
	tr, _ := BuildTrace([]Span{sp("t1", "a", "a", "svc", "q", 0, 10)})
	if tr.Valid {
		t.Fatal("self-referencing trace must be invalid")
	}
}

// Trace active time is the union of span intervals, not the sum.
func TestActiveTimeUnion(t *testing.T) {
	tr, _ := BuildTrace([]Span{
		sp("t1", "a", "", "svc", "q", 0, 50),
		sp("t1", "b", "", "svc", "q", 40, 90),
		sp("t1", "c", "", "svc", "q", 200, 210),
	})
	if tr.ActiveUs != 100 {
		t.Fatalf("active = %d, want 100", tr.ActiveUs)
	}
}

func TestParseRejectsEndBeforeStart(t *testing.T) {
	r := strings.NewReader(`{"trace_id":"t","span_id":"a","service":"s","operation":"o","start_us":10,"end_us":5}` + "\n")
	if _, err := ParseJSONL(r); err == nil {
		t.Fatal("expected error for end < start")
	}
}

func TestParseOutOfOrderOK(t *testing.T) {
	r := strings.NewReader(
		`{"trace_id":"t","span_id":"b","parent_id":"a","service":"s","operation":"o","start_us":1,"end_us":2}` + "\n" +
			`{"trace_id":"t","span_id":"a","service":"s","operation":"o","start_us":0,"end_us":3}` + "\n")
	spans, err := ParseJSONL(r)
	if err != nil {
		t.Fatal(err)
	}
	tr, _ := BuildTrace(spans)
	if len(tr.Roots) != 1 || tr.Roots[0].Span.SpanID != "a" {
		t.Fatal("out-of-order input not reconstructed correctly")
	}
}
