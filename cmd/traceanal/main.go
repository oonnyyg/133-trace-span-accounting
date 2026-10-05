// Command traceanal analyzes offline call-chain JSONL records.
//
//	traceanal analyze -in spans.jsonl [-spans out.csv] [-summary out.json]
//	traceanal compare -a old.jsonl -b new.jsonl [-out diff.json]
package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"example.com/trace-span-accounting/trace"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "analyze":
		err = runAnalyze(os.Args[2:])
	case "compare":
		err = runCompare(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  traceanal analyze -in spans.jsonl [-spans spans.csv] [-summary summary.json]
  traceanal compare -a batchA.jsonl -b batchB.jsonl [-out diff.json]`)
}

func flagValue(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func loadSpans(path string) ([]*trace.Span, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return trace.ParseJSONL(f)
}

func summarizeFile(path string) (trace.Summary, []*trace.Trace, error) {
	spans, err := loadSpans(path)
	if err != nil {
		return trace.Summary{}, nil, err
	}
	traces, anomalies, invalid := trace.Build(spans)
	return trace.Summarize(traces, anomalies, invalid), traces, nil
}

func runAnalyze(args []string) error {
	in := flagValue(args, "-in")
	if in == "" {
		return fmt.Errorf("analyze requires -in")
	}
	sum, traces, err := summarizeFile(in)
	if err != nil {
		return err
	}
	if p := flagValue(args, "-spans"); p != "" {
		if err := writeSpansCSV(p, traces); err != nil {
			return err
		}
	}
	return writeJSON(flagValue(args, "-summary"), sum)
}

func runCompare(args []string) error {
	aPath, bPath := flagValue(args, "-a"), flagValue(args, "-b")
	if aPath == "" || bPath == "" {
		return fmt.Errorf("compare requires -a and -b")
	}
	sumA, _, err := summarizeFile(aPath)
	if err != nil {
		return err
	}
	sumB, _, err := summarizeFile(bPath)
	if err != nil {
		return err
	}
	matched, added, removed := trace.Compare(sumA, sumB)
	return writeJSON(flagValue(args, "-out"), map[string]any{
		"matched": matched,
		"new":     added,
		"removed": removed,
	})
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if path == "" {
		_, err = os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// writeSpansCSV exports one row per span, sorted by (trace id, start, span id).
func writeSpansCSV(path string, traces []*trace.Trace) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{
		"trace_id", "span_id", "parent_id", "service", "operation",
		"start_us", "end_us", "inclusive_us", "exclusive_us",
		"trace_incomplete",
	}); err != nil {
		return err
	}
	for _, tr := range traces {
		for _, n := range tr.Nodes {
			s := n.Span
			rec := []string{
				s.TraceID, s.SpanID, s.ParentID, s.Service, s.Operation,
				strconv.FormatInt(s.StartUs, 10),
				strconv.FormatInt(s.EndUs, 10),
				strconv.FormatInt(s.Duration(), 10),
				strconv.FormatInt(n.ExclusiveUs, 10),
				strconv.FormatBool(tr.Incomplete),
			}
			if err := w.Write(rec); err != nil {
				return err
			}
		}
	}
	return w.Error()
}
