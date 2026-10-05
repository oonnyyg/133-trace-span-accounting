package trace

import (
	"strings"
	"testing"
)

func mustParse(t *testing.T, lines ...string) []*Span {
	t.Helper()
	spans, err := ParseJSONL(strings.NewReader(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatalf("ParseJSONL: %v", err)
	}
	return spans
}

func nodeBySpanID(tr *Trace, id string) *Node {
	for _, n := range tr.Nodes {
		if n.Span.SpanID == id {
			return n
		}
	}
	return nil
}

// Parallel children overlap in time; their union must be deducted once.
// A grandchild inside a child must not be deducted again at the root.
func TestExclusiveTimeParallelChildren(t *testing.T) {
	spans := mustParse(t,
		`{"trace_id":"t1","span_id":"root","service":"api","operation":"handle","start_us":0,"end_us":100}`,
		`{"trace_id":"t1","span_id":"a","parent_id":"root","service":"db","operation":"q1","start_us":10,"end_us":60}`,
		`{"trace_id":"t1","span_id":"b","parent_id":"root","service":"db","operation":"q2","start_us":40,"end_us":80}`,
		`{"trace_id":"t1","span_id":"g","parent_id":"a","service":"cache","operation":"get","start_us":20,"end_us":30}`,
	)
	traces, anomalies, invalid := Build(spans)
	if invalid != 0 || len(traces) != 1 {
		t.Fatalf("got %d traces, %d invalid", len(traces), invalid)
	}
	tr := traces[0]
	// children union: [10,80) = 70; root exclusive = 100-70 = 30.
	if got := nodeBySpanID(tr, "root").ExclusiveUs; got != 30 {
		t.Errorf("root exclusive = %d, want 30", got)
	}
	// child a: 50 - [20,30) = 40; grandchild deducted only at a.
	if got := nodeBySpanID(tr, "a").ExclusiveUs; got != 40 {
		t.Errorf("a exclusive = %d, want 40", got)
	}
	if got := nodeBySpanID(tr, "b").ExclusiveUs; got != 40 {
		t.Errorf("b exclusive = %d, want 40", got)
	}
	// active time: union of all intervals = [0,100) = 100, not the sum 230.
	if tr.ActiveUs != 100 {
		t.Errorf("active = %d, want 100", tr.ActiveUs)
	}
	for _, a := range anomalies {
		t.Errorf("unexpected anomaly: %+v", a)
	}
}

// Out-of-order input and multiple roots in one trace.
func TestOutOfOrderAndMultipleRoots(t *testing.T) {
	spans := mustParse(t,
		`{"trace_id":"t1","span_id":"c","parent_id":"r1","service":"s","operation":"op","start_us":5,"end_us":8}`,
		`{"trace_id":"t1","span_id":"r2","service":"s","operation":"op2","start_us":20,"end_us":30}`,
		`{"trace_id":"t1","span_id":"r1","service":"s","operation":"op","start_us":0,"end_us":10}`,
	)
	traces, _, invalid := Build(spans)
	if invalid != 0 || len(traces) != 1 {
		t.Fatalf("got %d traces, %d invalid", len(traces), invalid)
	}
	tr := traces[0]
	if len(tr.Roots) != 2 || tr.Roots[0].Span.SpanID != "r1" || tr.Roots[1].Span.SpanID != "r2" {
		t.Fatalf("roots not as expected: %+v", tr.Roots)
	}
	if got := nodeBySpanID(tr, "r1").ExclusiveUs; got != 7 {
		t.Errorf("r1 exclusive = %d, want 7", got)
	}
	// active = [0,10) ∪ [20,30) = 20.
	if tr.ActiveUs != 20 {
		t.Errorf("active = %d, want 20", tr.ActiveUs)
	}
}

// Missing parent: orphan branch kept, trace marked incomplete, no fake parent.
func TestMissingParentOrphan(t *testing.T) {
	spans := mustParse(t,
		`{"trace_id":"t1","span_id":"root","service":"s","operation":"op","start_us":0,"end_us":10}`,
		`{"trace_id":"t1","span_id":"orphan","parent_id":"ghost","service":"s","operation":"op2","start_us":2,"end_us":5}`,
	)
	traces, anomalies, invalid := Build(spans)
	if invalid != 0 || len(traces) != 1 {
		t.Fatalf("got %d traces, %d invalid", len(traces), invalid)
	}
	tr := traces[0]
	if !tr.Incomplete {
		t.Error("trace should be marked incomplete")
	}
	if len(tr.Roots) != 2 {
		t.Fatalf("expected 2 roots (orphan kept), got %d", len(tr.Roots))
	}
	if nodeBySpanID(tr, "orphan").Parent != nil {
		t.Error("orphan must not get a fabricated parent")
	}
	found := false
	for _, a := range anomalies {
		if a.Kind == "missing_parent" && a.SpanID == "orphan" {
			found = true
		}
	}
	if !found {
		t.Error("expected missing_parent anomaly")
	}
}
