package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
)

func main() {
	input := flag.String("input", "", "JSONL span records file (required)")
	compare := flag.String("compare", "", "optional second JSONL file to compare against -input")
	csvOut := flag.String("csv", "", "write per-span CSV to this file ('-' for stdout)")
	jsonOut := flag.String("json", "", "write JSON summary to this file ('-' for stdout)")
	flag.Parse()
	if *input == "" {
		fmt.Fprintln(os.Stderr, "error: -input is required")
		flag.Usage()
		os.Exit(2)
	}

	base, err := loadBatch(*input)
	if err != nil {
		fatal(err)
	}
	summary := map[string]any{"batch": base}

	var next *Batch
	if *compare != "" {
		next, err = loadBatch(*compare)
		if err != nil {
			fatal(err)
		}
		summary["compare_batch"] = next
		summary["diff"] = CompareBatches(base, next)
	}

	if *csvOut != "" {
		if err := writeCSV(*csvOut, base, next); err != nil {
			fatal(err)
		}
	}
	if *jsonOut != "" {
		if err := writeJSON(*jsonOut, summary); err != nil {
			fatal(err)
		}
	}
	if *csvOut == "" && *jsonOut == "" {
		if err := writeJSON("-", summary); err != nil {
			fatal(err)
		}
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

func loadBatch(path string) (*Batch, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	spans, err := ParseJSONL(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return AnalyzeBatch(spans)
}

func writerFor(path string) (*os.File, error) {
	if path == "-" {
		return os.Stdout, nil
	}
	return os.Create(path)
}

// writeCSV emits one row per span, ordered by trace id then span id.
func writeCSV(path string, batches ...*Batch) error {
	f, err := writerFor(path)
	if err != nil {
		return err
	}
	if f != os.Stdout {
		defer f.Close()
	}
	w := csv.NewWriter(f)
	defer w.Flush()
	header := []string{"batch", "trace_id", "span_id", "parent_id", "service", "operation",
		"start_us", "end_us", "inclusive_us", "exclusive_us", "trace_complete", "trace_active_us"}
	if err := w.Write(header); err != nil {
		return err
	}
	for i, b := range batches {
		if b == nil {
			continue
		}
		label := "base"
		if i == 1 {
			label = "compare"
		}
		for _, tr := range b.Traces {
			nodes := tr.All()
			sort.Slice(nodes, func(x, y int) bool { return nodes[x].Span.SpanID < nodes[y].Span.SpanID })
			for _, n := range nodes {
				row := []string{
					label, tr.ID, n.Span.SpanID, n.Span.ParentID, n.Span.Service, n.Span.Operation,
					strconv.FormatInt(n.Span.StartUs, 10), strconv.FormatInt(n.Span.EndUs, 10),
					strconv.FormatInt(n.Span.Duration(), 10), strconv.FormatInt(n.Exclusive, 10),
					strconv.FormatBool(tr.Complete), strconv.FormatInt(tr.ActiveUs, 10),
				}
				if err := w.Write(row); err != nil {
					return err
				}
			}
		}
	}
	return w.Error()
}

func writeJSON(path string, v any) error {
	f, err := writerFor(path)
	if err != nil {
		return err
	}
	if f != os.Stdout {
		defer f.Close()
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
