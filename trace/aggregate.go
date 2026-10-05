package trace

import "sort"

// OpKey identifies a service/operation pair.
type OpKey struct {
	Service   string `json:"service"`
	Operation string `json:"operation"`
}

// OpStat aggregates spans for one service/operation.
type OpStat struct {
	OpKey
	Calls        int     `json:"calls"`
	InclusiveUs  int64   `json:"inclusive_us"`
	ExclusiveUs  int64   `json:"exclusive_us"`
	AvgInclusive float64 `json:"avg_inclusive_us"`
	AvgExclusive float64 `json:"avg_exclusive_us"`
}

// Summary is the aggregate result of one batch.
type Summary struct {
	TotalSpans       int       `json:"total_spans"`
	ValidTraces      int       `json:"valid_traces"`
	InvalidTraces    int       `json:"invalid_traces"`
	IncompleteTraces int       `json:"incomplete_traces"`
	TotalActiveUs    int64     `json:"total_active_us"`
	Operations       []OpStat  `json:"operations"`
	Anomalies        []Anomaly `json:"anomalies"`
}

// Summarize aggregates valid traces by service/operation, sorted by
// (service, operation).
func Summarize(traces []*Trace, anomalies []Anomaly, invalidCount int) Summary {
	sum := Summary{Anomalies: anomalies, InvalidTraces: invalidCount}
	byOp := make(map[OpKey]*OpStat)
	for _, tr := range traces {
		sum.ValidTraces++
		if tr.Incomplete {
			sum.IncompleteTraces++
		}
		sum.TotalActiveUs += tr.ActiveUs
		for _, n := range tr.Nodes {
			sum.TotalSpans++
			k := OpKey{Service: n.Span.Service, Operation: n.Span.Operation}
			st, ok := byOp[k]
			if !ok {
				st = &OpStat{OpKey: k}
				byOp[k] = st
			}
			st.Calls++
			st.InclusiveUs += n.Span.Duration()
			st.ExclusiveUs += n.ExclusiveUs
		}
	}
	for _, st := range byOp {
		st.AvgInclusive = float64(st.InclusiveUs) / float64(st.Calls)
		st.AvgExclusive = float64(st.ExclusiveUs) / float64(st.Calls)
		sum.Operations = append(sum.Operations, *st)
	}
	sort.Slice(sum.Operations, func(i, j int) bool {
		a, b := sum.Operations[i], sum.Operations[j]
		if a.Service != b.Service {
			return a.Service < b.Service
		}
		return a.Operation < b.Operation
	})
	return sum
}

// OpDiff compares one operation across two batches.
type OpDiff struct {
	OpKey
	Status            string  `json:"status"` // "matched", "new", "removed"
	CallsA            int     `json:"calls_a"`
	CallsB            int     `json:"calls_b"`
	CallsDelta        int     `json:"calls_delta"`
	AvgExclusiveA     float64 `json:"avg_exclusive_us_a,omitempty"`
	AvgExclusiveB     float64 `json:"avg_exclusive_us_b,omitempty"`
	AvgExclusiveDelta float64 `json:"avg_exclusive_delta_us,omitempty"`
}

// Compare aligns two summaries by service/operation. Operations present in
// only one batch are listed separately and are never treated as zero-time
// samples in the delta computation.
func Compare(a, b Summary) (matched, added, removed []OpDiff) {
	matched, added, removed = []OpDiff{}, []OpDiff{}, []OpDiff{}
	mapA := indexByOp(a.Operations)
	mapB := indexByOp(b.Operations)
	keys := make(map[OpKey]bool)
	for k := range mapA {
		keys[k] = true
	}
	for k := range mapB {
		keys[k] = true
	}
	sorted := make([]OpKey, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Service != sorted[j].Service {
			return sorted[i].Service < sorted[j].Service
		}
		return sorted[i].Operation < sorted[j].Operation
	})
	for _, k := range sorted {
		sa, inA := mapA[k]
		sb, inB := mapB[k]
		switch {
		case inA && inB:
			matched = append(matched, OpDiff{
				OpKey: k, Status: "matched",
				CallsA: sa.Calls, CallsB: sb.Calls,
				CallsDelta:        sb.Calls - sa.Calls,
				AvgExclusiveA:     sa.AvgExclusive,
				AvgExclusiveB:     sb.AvgExclusive,
				AvgExclusiveDelta: sb.AvgExclusive - sa.AvgExclusive,
			})
		case inB:
			added = append(added, OpDiff{
				OpKey: k, Status: "new",
				CallsB: sb.Calls, CallsDelta: sb.Calls,
				AvgExclusiveB: sb.AvgExclusive,
			})
		default:
			removed = append(removed, OpDiff{
				OpKey: k, Status: "removed",
				CallsA: sa.Calls, CallsDelta: -sa.Calls,
				AvgExclusiveA: sa.AvgExclusive,
			})
		}
	}
	return matched, added, removed
}

func indexByOp(ops []OpStat) map[OpKey]OpStat {
	m := make(map[OpKey]OpStat, len(ops))
	for _, o := range ops {
		m[o.OpKey] = o
	}
	return m
}
