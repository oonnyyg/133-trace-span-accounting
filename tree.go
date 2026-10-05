package main

import (
	"encoding/json"
	"sort"
)

// Node is a span placed in the reconstructed call tree.
type Node struct {
	Span       Span
	Children   []*Node
	Parent     *Node
	Exclusive  int64
	OutOfRange bool
}

// MarshalJSON flattens the tree; Parent is omitted to avoid cycles.
func (n *Node) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Span      Span    `json:"span"`
		Exclusive int64   `json:"exclusive_us"`
		Children  []*Node `json:"children,omitempty"`
	}{n.Span, n.Exclusive, n.Children})
}

// Trace is the analysis result for one trace id.
type Trace struct {
	ID         string
	Roots      []*Node
	Complete   bool
	Valid      bool
	InvalidWhy string
	ActiveUs   int64
}

// All returns every node in deterministic (span id) order.
func (t *Trace) All() []*Node {
	var nodes []*Node
	var walk func(n *Node)
	walk = func(n *Node) {
		nodes = append(nodes, n)
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, r := range t.Roots {
		walk(r)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Span.SpanID < nodes[j].Span.SpanID })
	return nodes
}

// BuildTrace reconstructs the call forest for one trace. Spans whose
// parent id is non-empty but unknown become synthetic roots of orphan
// branches and the trace is marked incomplete; no fake parent is
// created. Self-references and parent/child cycles invalidate the
// whole trace without affecting other traces.
func BuildTrace(spans []Span) (*Trace, []Anomaly) {
	tr := &Trace{ID: spans[0].TraceID, Complete: true, Valid: true}
	var anomalies []Anomaly

	nodes := make(map[string]*Node, len(spans))
	for _, s := range spans {
		nodes[s.SpanID] = &Node{Span: s}
	}
	// Attach children; missing parents stay as orphan roots.
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		n := nodes[id]
		pid := n.Span.ParentID
		if pid == "" {
			tr.Roots = append(tr.Roots, n)
			continue
		}
		p, ok := nodes[pid]
		if !ok {
			tr.Complete = false
			tr.Roots = append(tr.Roots, n)
			anomalies = append(anomalies, Anomaly{
				Kind: "missing_parent", TraceID: tr.ID, SpanID: id,
				Detail: "parent span " + pid + " not present; kept as orphan root",
			})
			continue
		}
		n.Parent = p
		p.Children = append(p.Children, n)
	}
	// Cycle detection: walk ancestors of each node.
	for _, id := range ids {
		seen := map[string]bool{}
		for n := nodes[id]; n != nil; n = n.Parent {
			if seen[n.Span.SpanID] {
				tr.Valid = false
				tr.InvalidWhy = "cycle detected involving span " + n.Span.SpanID
				return tr, anomalies
			}
			seen[n.Span.SpanID] = true
		}
	}
	// Deterministic child order: by start, then span id.
	for _, n := range nodes {
		sort.Slice(n.Children, func(i, j int) bool {
			a, b := n.Children[i].Span, n.Children[j].Span
			if a.StartUs != b.StartUs {
				return a.StartUs < b.StartUs
			}
			return a.SpanID < b.SpanID
		})
	}
	sort.Slice(tr.Roots, func(i, j int) bool {
		a, b := tr.Roots[i].Span, tr.Roots[j].Span
		if a.StartUs != b.StartUs {
			return a.StartUs < b.StartUs
		}
		return a.SpanID < b.SpanID
	})

	var intervals [][2]int64
	for _, n := range nodes {
		n.Exclusive, n.OutOfRange = exclusiveTime(n)
		if n.OutOfRange {
			anomalies = append(anomalies, Anomaly{
				Kind: "child_out_of_range", TraceID: tr.ID, SpanID: n.Span.SpanID,
				Detail: "child interval extends outside parent; clipped to parent range",
			})
		}
		intervals = append(intervals, [2]int64{n.Span.StartUs, n.Span.EndUs})
	}
	tr.ActiveUs = unionLength(intervals)
	return tr, anomalies
}

// exclusiveTime = own duration minus the union of direct child
// intervals clipped to the parent range. Overlapping children are
// counted once; grandchildren are never subtracted here.
func exclusiveTime(n *Node) (int64, bool) {
	start, end := n.Span.StartUs, n.Span.EndUs
	outOfRange := false
	var clips [][2]int64
	for _, c := range n.Children {
		cs, ce := c.Span.StartUs, c.Span.EndUs
		if cs < start || ce > end {
			outOfRange = true
		}
		if cs < start {
			cs = start
		}
		if ce > end {
			ce = end
		}
		if ce > cs {
			clips = append(clips, [2]int64{cs, ce})
		}
	}
	return n.Span.Duration() - unionLength(clips), outOfRange
}

// unionLength returns the total covered length of [start,end) intervals.
func unionLength(iv [][2]int64) int64 {
	if len(iv) == 0 {
		return 0
	}
	sorted := make([][2]int64, len(iv))
	copy(sorted, iv)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i][0] != sorted[j][0] {
			return sorted[i][0] < sorted[j][0]
		}
		return sorted[i][1] < sorted[j][1]
	})
	var total, curS, curE int64
	curS, curE = sorted[0][0], sorted[0][1]
	for _, p := range sorted[1:] {
		if p[0] > curE {
			total += curE - curS
			curS, curE = p[0], p[1]
		} else if p[1] > curE {
			curE = p[1]
		}
	}
	return total + curE - curS
}
