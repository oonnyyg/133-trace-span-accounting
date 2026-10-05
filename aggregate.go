package main

import "sort"

// OpKey identifies a service/operation pair.
type OpKey struct {
	Service   string `json:"service"`
	Operation string `json:"operation"`
}

// OpStat aggregates spans for one service/operation.
type OpStat struct {
	OpKey
	Calls        int   `json:"calls"`
	InclusiveUs  int64 `json:"inclusive_us"`
	ExclusiveUs  int64 `json:"exclusive_us"`
}

// AvgExclusiveUs is the mean exclusive time per call.
func (s OpStat) AvgExclusiveUs() float64 {
	if s.Calls == 0 {
		return 0
	}
	return float64(s.ExclusiveUs) / float64(s.Calls)
}

// Batch is the full analysis result for one input file.
type Batch struct {
	Traces   []*Trace  `json:"traces"`
	Stats    []OpStat  `json:"stats"`
	Anomalies []Anomaly `json:"anomalies"`
}

// AnalyzeBatch groups spans into traces, rebuilds trees and aggregates
// per service/operation. Invalid traces are excluded from stats but
// reported as anomalies; other traces keep being analyzed.
func AnalyzeBatch(spans []Span) (*Batch, error) {
	byTrace, ids, err := GroupByTrace(spans)
	if err != nil {
		return nil, err
	}
	b := &Batch{}
	agg := make(map[OpKey]*OpStat)
	for _, id := range ids {
		tr, anomalies := BuildTrace(byTrace[id])
		b.Anomalies = append(b.Anomalies, anomalies...)
		if !tr.Valid {
			b.Anomalies = append(b.Anomalies, Anomaly{
				Kind: "invalid_trace", TraceID: id, Detail: tr.InvalidWhy,
			})
			continue
		}
		b.Traces = append(b.Traces, tr)
		for _, n := range tr.All() {
			k := OpKey{Service: n.Span.Service, Operation: n.Span.Operation}
			st, ok := agg[k]
			if !ok {
				st = &OpStat{OpKey: k}
				agg[k] = st
			}
			st.Calls++
			st.InclusiveUs += n.Span.Duration()
			st.ExclusiveUs += n.Exclusive
		}
	}
	for _, st := range agg {
		b.Stats = append(b.Stats, *st)
	}
	sort.Slice(b.Stats, func(i, j int) bool {
		if b.Stats[i].Service != b.Stats[j].Service {
			return b.Stats[i].Service < b.Stats[j].Service
		}
		return b.Stats[i].Operation < b.Stats[j].Operation
	})
	return b, nil
}
