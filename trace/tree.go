package trace

import "sort"

// Node is a span placed into its trace tree.
type Node struct {
	Span        *Span
	Parent      *Node
	Children    []*Node
	ExclusiveUs int64
}

// Trace is the analysis result for one trace ID.
type Trace struct {
	ID         string
	Roots      []*Node // forest: multiple roots and orphan branches
	Nodes      []*Node // all nodes, sorted by (start, span id)
	Valid      bool
	Incomplete bool  // at least one parent_id was not found
	ActiveUs   int64 // union of all span intervals
}

// Build reconstructs per-trace forests from arbitrarily ordered spans.
// Invalid traces (duplicate span IDs, self-parenting, cycles) are excluded
// from Valid results but reported via anomalies; other traces still analyze.
func Build(spans []*Span) (traces []*Trace, anomalies []Anomaly, invalid int) {
	ids, byTrace := groupByTrace(spans)
	for _, id := range ids {
		tr, anoms := buildTrace(id, byTrace[id])
		anomalies = append(anomalies, anoms...)
		if tr != nil {
			traces = append(traces, tr)
		} else {
			invalid++
		}
	}
	return traces, anomalies, invalid
}

func buildTrace(id string, spans []*Span) (*Trace, []Anomaly) {
	var anomalies []Anomaly
	bySpanID := make(map[string]*Node, len(spans))
	for _, s := range spans {
		if _, dup := bySpanID[s.SpanID]; dup {
			anomalies = append(anomalies, Anomaly{
				TraceID: id, SpanID: s.SpanID, Kind: "duplicate_span_id",
				Detail: "span id appears more than once; trace invalidated",
			})
			return nil, anomalies
		}
		bySpanID[s.SpanID] = &Node{Span: s}
	}
	tr := &Trace{ID: id, Valid: true}

	// Link parents. Missing parents keep the node as an orphan root and
	// mark the trace incomplete; no synthetic parent is fabricated.
	for _, n := range bySpanID {
		pid := n.Span.ParentID
		if pid == "" {
			tr.Roots = append(tr.Roots, n)
			continue
		}
		if pid == n.Span.SpanID {
			anomalies = append(anomalies, Anomaly{
				TraceID: id, SpanID: n.Span.SpanID, Kind: "self_parent",
				Detail: "span is its own parent; trace invalidated",
			})
			return nil, anomalies
		}
		p, ok := bySpanID[pid]
		if !ok {
			tr.Incomplete = true
			tr.Roots = append(tr.Roots, n)
			anomalies = append(anomalies, Anomaly{
				TraceID: id, SpanID: n.Span.SpanID, Kind: "missing_parent",
				Detail: "parent span " + pid + " not found; kept as orphan root",
			})
			continue
		}
		n.Parent = p
		p.Children = append(p.Children, n)
	}

	// Cycle detection: walk parent pointers with a visited set.
	for _, n := range bySpanID {
		seen := map[*Node]bool{}
		for cur := n; cur != nil; cur = cur.Parent {
			if seen[cur] {
				anomalies = append(anomalies, Anomaly{
					TraceID: id, SpanID: cur.Span.SpanID, Kind: "cycle",
					Detail: "parent chain forms a cycle; trace invalidated",
				})
				return nil, anomalies
			}
			seen[cur] = true
		}
	}

	for _, n := range bySpanID {
		sortChildren(n)
		n.ExclusiveUs, anomalies = exclusiveTime(n, anomalies)
		tr.Nodes = append(tr.Nodes, n)
	}
	sort.Slice(tr.Roots, func(i, j int) bool {
		a, b := tr.Roots[i].Span, tr.Roots[j].Span
		if a.StartUs != b.StartUs {
			return a.StartUs < b.StartUs
		}
		return a.SpanID < b.SpanID
	})
	sort.Slice(tr.Nodes, func(i, j int) bool {
		a, b := tr.Nodes[i].Span, tr.Nodes[j].Span
		if a.StartUs != b.StartUs {
			return a.StartUs < b.StartUs
		}
		return a.SpanID < b.SpanID
	})
	tr.ActiveUs = unionLength(tr.Nodes)
	return tr, anomalies
}

func sortChildren(n *Node) {
	sort.Slice(n.Children, func(i, j int) bool {
		a, b := n.Children[i].Span, n.Children[j].Span
		if a.StartUs != b.StartUs {
			return a.StartUs < b.StartUs
		}
		return a.SpanID < b.SpanID
	})
}

// exclusiveTime subtracts the union of direct-child intervals (clipped to
// the parent interval) from the parent's own duration. Overlapping children
// are not double-counted; grandchildren are not subtracted again.
func exclusiveTime(n *Node, anomalies []Anomaly) (int64, []Anomaly) {
	s := n.Span
	if len(n.Children) == 0 {
		return s.Duration(), anomalies
	}
	iv := make([][2]int64, 0, len(n.Children))
	for _, c := range n.Children {
		cs, ce := c.Span.StartUs, c.Span.EndUs
		if cs < s.StartUs || ce > s.EndUs {
			anomalies = append(anomalies, Anomaly{
				TraceID: s.TraceID, SpanID: c.Span.SpanID, Kind: "child_out_of_bounds",
				Detail: "child interval exceeds parent " + s.SpanID + "; clipped",
			})
		}
		if cs < s.StartUs {
			cs = s.StartUs
		}
		if ce > s.EndUs {
			ce = s.EndUs
		}
		if ce > cs {
			iv = append(iv, [2]int64{cs, ce})
		}
	}
	return s.Duration() - mergeUnion(iv), anomalies
}

// unionLength returns the total length of the union of all node intervals.
func unionLength(nodes []*Node) int64 {
	iv := make([][2]int64, 0, len(nodes))
	for _, n := range nodes {
		if n.Span.EndUs > n.Span.StartUs {
			iv = append(iv, [2]int64{n.Span.StartUs, n.Span.EndUs})
		}
	}
	return mergeUnion(iv)
}

func mergeUnion(iv [][2]int64) int64 {
	if len(iv) == 0 {
		return 0
	}
	sort.Slice(iv, func(i, j int) bool { return iv[i][0] < iv[j][0] })
	var total int64
	curS, curE := iv[0][0], iv[0][1]
	for _, p := range iv[1:] {
		if p[0] > curE {
			total += curE - curS
			curS, curE = p[0], p[1]
		} else if p[1] > curE {
			curE = p[1]
		}
	}
	return total + curE - curS
}
