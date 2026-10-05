package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// Span is a single service call record.
type Span struct {
	TraceID   string `json:"trace_id"`
	SpanID    string `json:"span_id"`
	ParentID  string `json:"parent_id"`
	Service   string `json:"service"`
	Operation string `json:"operation"`
	StartUs   int64  `json:"start_us"`
	EndUs     int64  `json:"end_us"`
}

// Duration is the raw span length in microseconds.
func (s Span) Duration() int64 { return s.EndUs - s.StartUs }

// Anomaly describes a non-fatal data problem found during analysis.
type Anomaly struct {
	Kind    string `json:"kind"`
	TraceID string `json:"trace_id"`
	SpanID  string `json:"span_id,omitempty"`
	Detail  string `json:"detail"`
}

// ParseJSONL reads JSONL span records. Invalid records abort parsing
// with an error since a single corrupt line makes the batch unreliable.
func ParseJSONL(r io.Reader) ([]Span, error) {
	var spans []Span
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		data := sc.Bytes()
		if len(data) == 0 {
			continue
		}
		var s Span
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		if s.TraceID == "" || s.SpanID == "" {
			return nil, fmt.Errorf("line %d: trace_id and span_id are required", line)
		}
		if s.EndUs < s.StartUs {
			return nil, fmt.Errorf("line %d: span %s ends before it starts", line, s.SpanID)
		}
		spans = append(spans, s)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return spans, nil
}

// GroupByTrace splits spans by trace id, rejecting duplicate span ids
// within a trace. Traces are returned in sorted id order.
func GroupByTrace(spans []Span) (map[string][]Span, []string, error) {
	byTrace := make(map[string][]Span)
	seen := make(map[string]map[string]bool)
	for _, s := range spans {
		if seen[s.TraceID] == nil {
			seen[s.TraceID] = make(map[string]bool)
		}
		if seen[s.TraceID][s.SpanID] {
			return nil, nil, fmt.Errorf("trace %s: duplicate span id %q", s.TraceID, s.SpanID)
		}
		seen[s.TraceID][s.SpanID] = true
		byTrace[s.TraceID] = append(byTrace[s.TraceID], s)
	}
	ids := make([]string, 0, len(byTrace))
	for id := range byTrace {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return byTrace, ids, nil
}
