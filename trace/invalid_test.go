package trace

import (
	"strings"
	"testing"
)

// Self-parent and parent-child cycles invalidate only their own trace.
func TestInvalidTracesDoNotBlockOthers(t *testing.T) {
	spans := mustParse(t,
		`{"trace_id":"bad-self","span_id":"x","parent_id":"x","service":"s","operation":"op","start_us":0,"end_us":1}`,
		`{"trace_id":"bad-cycle","span_id":"a","parent_id":"b","service":"s","operation":"op","start_us":0,"end_us":5}`,
		`{"trace_id":"bad-cycle","span_id":"b","parent_id":"a","service":"s","operation":"op","start_us":1,"end_us":4}`,
		`{"trace_id":"bad-dup","span_id":"d","service":"s","operation":"op","start_us":0,"end_us":1}`,
		`{"trace_id":"bad-dup","span_id":"d","service":"s","operation":"op","start_us":2,"end_us":3}`,
		`{"trace_id":"good","span_id":"r","service":"s","operation":"op","start_us":0,"end_us":10}`,
	)
	traces, anomalies, invalid := Build(spans)
	if invalid != 3 {
		t.Errorf("invalid = %d, want 3", invalid)
	}
	if len(traces) != 1 || traces[0].ID != "good" {
		t.Fatalf("expected only 'good' trace, got %+v", traces)
	}
	kinds := map[string]bool{}
	for _, a := range anomalies {
		kinds[a.Kind] = true
	}
	for _, want := range []string{"self_parent", "cycle", "duplicate_span_id"} {
		if !kinds[want] {
			t.Errorf("missing anomaly kind %q", want)
		}
	}
}

// Child interval outside the parent is clipped for exclusive time and
// reported as an anomaly.
func TestChildOutOfBoundsClipped(t *testing.T) {
	spans := mustParse(t,
		`{"trace_id":"t1","span_id":"root","service":"s","operation":"op","start_us":10,"end_us":20}`,
		`{"trace_id":"t1","span_id":"c","parent_id":"root","service":"s","operation":"op2","start_us":5,"end_us":15}`,
	)
	traces, anomalies, invalid := Build(spans)
	if invalid != 0 || len(traces) != 1 {
		t.Fatalf("got %d traces, %d invalid", len(traces), invalid)
	}
	// clipped child interval [10,15) -> exclusive = 10 - 5 = 5.
	if got := nodeBySpanID(traces[0], "root").ExclusiveUs; got != 5 {
		t.Errorf("root exclusive = %d, want 5", got)
	}
	found := false
	for _, a := range anomalies {
		if a.Kind == "child_out_of_bounds" && a.SpanID == "c" {
			found = true
		}
	}
	if !found {
		t.Error("expected child_out_of_bounds anomaly")
	}
}

// end < start is a hard parse error.
func TestEndBeforeStartRejected(t *testing.T) {
	_, err := ParseJSONL(strings.NewReader(
		`{"trace_id":"t","span_id":"x","service":"s","operation":"o","start_us":5,"end_us":4}`))
	if err == nil {
		t.Fatal("expected error for end < start")
	}
}
