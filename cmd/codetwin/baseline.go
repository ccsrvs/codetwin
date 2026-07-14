package main

// CLI glue for the clone watchlist (roadmap bet #5): --update-baseline
// writes a snapshot of the visible clusters after a normal scan;
// --baseline diffs the scan against a stored snapshot and turns drift
// into a CI gate (exit 1). Snapshots record post-suppression clusters —
// exactly what the report shows — so test segregation and
// --include-tests compose naturally: you snapshot what you see.
// Mixing modes between snapshot and compare is user error; the stored
// scan params catch it up front (see baseline.Params).

import (
	"fmt"
	"os"

	"github.com/ccsrvs/codetwin/internal/baseline"
	"github.com/ccsrvs/codetwin/internal/report"
	"github.com/ccsrvs/codetwin/internal/scan"
)

// loadBaselineForCompare loads and validates the snapshot behind
// --baseline before any file processing, so schema-version or
// scan-params mismatches fail fast with a clear error instead of after
// a full scan.
func loadBaselineForCompare(file string, current baseline.Params) baseline.Snapshot {
	snap, err := baseline.Load(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if mismatches := snap.Params.Mismatches(current); len(mismatches) > 0 {
		fmt.Fprintf(os.Stderr,
			"error: baseline %s was created with different scan parameters — the cluster sets are not comparable:\n", file)
		for _, m := range mismatches {
			fmt.Fprintf(os.Stderr, "  %s\n", m)
		}
		fmt.Fprintln(os.Stderr, "re-run with matching flags, or regenerate the snapshot with --update-baseline")
		os.Exit(1)
	}
	return snap
}

// buildBaselineSnapshot packages the prepared (post-suppression,
// report-visible) clusters as a baseline snapshot: member keys are
// line-range-stripped and made relative to the scan roots, and each
// member carries its normalized-token content hash.
func buildBaselineSnapshot(
	clusters []report.Cluster,
	snippets []scan.Snippet,
	roots []string,
	params baseline.Params,
) baseline.Snapshot {
	memberLists := make([][]string, len(clusters))
	for i, c := range clusters {
		memberLists[i] = c.Members
	}
	tokens := make(map[string][]string, len(snippets))
	for _, s := range snippets {
		tokens[s.Name] = s.Tokens
	}
	return baseline.Snapshot{
		SchemaVersion: baseline.SchemaVersion,
		ToolVersion:   buildVersion,
		Params:        params,
		Clusters:      baseline.BuildClusters(memberLists, tokens, baseline.NewKeyer(roots)),
	}
}

// finishBaseline runs after the normal report has been written to
// stdout. In --update-baseline mode it saves the snapshot (exit 1 on a
// write failure). In --baseline mode it prints one stderr line per
// drift event and exits 1 when any drift was detected — the CI gate.
func finishBaseline(updatePath, comparePath string, snap baseline.Snapshot, events []baseline.Event) {
	if updatePath != "" {
		if err := baseline.Save(updatePath, snap); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if comparePath == "" {
		return
	}
	for _, e := range events {
		fmt.Fprintln(os.Stderr, e.String())
	}
	if len(events) > 0 {
		os.Exit(1)
	}
}
