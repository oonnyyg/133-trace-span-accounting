package main

import "sort"

// Change classifies an operation across two batches.
type Change string

const (
	ChangeCommon      Change = "common"
	ChangeAppeared    Change = "appeared"
	ChangeDisappeared Change = "disappeared"
)

// OpDiff compares one service/operation between two batches.
type OpDiff struct {
	OpKey
	Change            Change  `json:"change"`
	BaseCalls         int     `json:"base_calls"`
	NewCalls          int     `json:"new_calls"`
	CallsDelta        int     `json:"calls_delta"`
	BaseAvgExclusive  float64 `json:"base_avg_exclusive_us,omitempty"`
	NewAvgExclusive   float64 `json:"new_avg_exclusive_us,omitempty"`
	AvgExclusiveDelta float64 `json:"avg_exclusive_delta_us,omitempty"`
}

// CompareBatches aligns stats by service/operation. Operations present
// in only one batch are listed separately as appeared/disappeared and
// are never treated as zero-duration samples.
func CompareBatches(base, next *Batch) []OpDiff {
	index := func(b *Batch) map[OpKey]OpStat {
		m := make(map[OpKey]OpStat, len(b.Stats))
		for _, s := range b.Stats {
			m[s.OpKey] = s
		}
		return m
	}
	bm, nm := index(base), index(next)
	keys := map[OpKey]bool{}
	for k := range bm {
		keys[k] = true
	}
	for k := range nm {
		keys[k] = true
	}
	var diffs []OpDiff
	for k := range keys {
		b, hasB := bm[k]
		n, hasN := nm[k]
		d := OpDiff{OpKey: k, BaseCalls: b.Calls, NewCalls: n.Calls}
		switch {
		case hasB && hasN:
			d.Change = ChangeCommon
			d.CallsDelta = n.Calls - b.Calls
			d.BaseAvgExclusive = b.AvgExclusiveUs()
			d.NewAvgExclusive = n.AvgExclusiveUs()
			d.AvgExclusiveDelta = d.NewAvgExclusive - d.BaseAvgExclusive
		case hasN:
			d.Change = ChangeAppeared
			d.NewAvgExclusive = n.AvgExclusiveUs()
		default:
			d.Change = ChangeDisappeared
			d.BaseAvgExclusive = b.AvgExclusiveUs()
		}
		diffs = append(diffs, d)
	}
	sort.Slice(diffs, func(i, j int) bool {
		if diffs[i].Service != diffs[j].Service {
			return diffs[i].Service < diffs[j].Service
		}
		return diffs[i].Operation < diffs[j].Operation
	})
	return diffs
}
