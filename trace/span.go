// Package trace implements offline call-chain analysis from JSONL span
// records. It rebuilds parent-child relationships per trace, computes
// exclusive (self) time, and aggregates per service/operation.
package trace

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// Span is a single call-chain fragment record.
type Span struct {
	TraceID   string `json:"trace_id"`
	SpanID    string `json:"span_id"`
	ParentID  string `json:"parent_id,omitempty"`
	Service   string `json:"service"`
	Operation string `json:"operation"`
	StartUs   int64  `json:"start_us"`
	EndUs     int64  `json:"end_us"`
}

// Duration is the span's own wall-clock length in microseconds.
func (s *Span) Duration() int64 { return s.EndUs - s.StartUs }

// Anomaly describes a non-fatal data problem found during analysis.
type Anomaly struct {
	TraceID string `json:"trace_id"`
	SpanID  string `json:"span_id,omitempty"`
	Kind    string `json:"kind"`
	Detail  string `json:"detail"`
}

// ParseJSONL reads newline-delimited span records. Blank lines are skipped.
// A record with end < start is a hard error; other structural problems are
// reported as anomalies during Build.
func ParseJSONL(r io.Reader) ([]*Span, error) {
	var spans []*Span
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		data := sc.Bytes()
		if line == 1 {
			data = trimBOM(data)
		}
		if len(trimSpace(data)) == 0 {
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
			return nil, fmt.Errorf("line %d: span %s ends before it starts (%d < %d)",
				line, s.SpanID, s.EndUs, s.StartUs)
		}
		spans = append(spans, &s)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return spans, nil
}

func trimBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\r') {
		b = b[1:]
	}
	for len(b) > 0 {
		c := b[len(b)-1]
		if c != ' ' && c != '\t' && c != '\r' {
			break
		}
		b = b[:len(b)-1]
	}
	return b
}

// groupByTrace returns trace IDs in sorted order and their spans.
func groupByTrace(spans []*Span) ([]string, map[string][]*Span) {
	byTrace := make(map[string][]*Span)
	for _, s := range spans {
		byTrace[s.TraceID] = append(byTrace[s.TraceID], s)
	}
	ids := make([]string, 0, len(byTrace))
	for id := range byTrace {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, byTrace
}
