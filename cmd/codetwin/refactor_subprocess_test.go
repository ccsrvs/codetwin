package main

// Subprocess CLI tests for the --suggest / --suggest-all flags. These
// build the binary, invoke it as a child process, and assert on the
// stdout/stderr/exit code surface — closing the gap that unit tests on
// emitSuggestion / buildSuggestionMap leave open (flag wiring,
// stdout/stderr discipline, exit codes, JSON schema as the binary
// actually emits it). This is the first *_subprocess_test.go in the
// repo and establishes the convention required by docs/roadmap.md
// (lines 264–265, 362) for every new bet.

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// subprocessBinary is the path to the codetwin binary built once for
// the whole package by TestMain. Empty when -short is set; in that
// case every subprocess test skips itself.
var subprocessBinary string

func TestMain(m *testing.M) {
	flag.Parse() // testing.Short() is unsafe before this point.
	code := func() int {
		if testing.Short() {
			return m.Run()
		}
		dir, err := os.MkdirTemp("", "codetwin-subproc-")
		if err != nil {
			fmt.Fprintf(os.Stderr, "subprocess test setup: %v\n", err)
			return 1
		}
		defer os.RemoveAll(dir)
		bin := filepath.Join(dir, "codetwin")
		out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
		if err != nil {
			fmt.Fprintf(os.Stderr, "subprocess build failed: %v\n%s\n", err, out)
			return 1
		}
		subprocessBinary = bin
		return m.Run()
	}()
	os.Exit(code)
}

func subprocessBin(t *testing.T) string {
	t.Helper()
	if subprocessBinary == "" {
		t.Skip("subprocess tests skipped in -short mode")
	}
	return subprocessBinary
}

// TestSuggest_JavaSimple_ExitsZeroAndPrintsDiff: discover the pair ID
// via --json, then invoke --suggest <id> against the same fixture.
// Asserts exit 0 and that stdout contains a unified-diff hunk header
// plus the helper signature.
func TestSuggest_JavaSimple_ExitsZeroAndPrintsDiff(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/refactor/java/simple"

	jsonOut, err := exec.Command(bin,
		"--threshold", "0.0",
		"--no-cache", "--no-progress",
		"--json", fixtureDir,
	).Output()
	if err != nil {
		t.Fatalf("--json discovery: %v\nstdout:\n%s", err, jsonOut)
	}
	var doc struct {
		Pairs []struct {
			ID string `json:"id"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(jsonOut, &doc); err != nil {
		t.Fatalf("parse JSON: %v\nstdout:\n%s", err, jsonOut)
	}
	if len(doc.Pairs) == 0 || doc.Pairs[0].ID == "" {
		t.Fatalf("expected at least one pair with an id, got:\n%s", jsonOut)
	}
	pairID := doc.Pairs[0].ID

	cmd := exec.Command(bin,
		"--no-cache", "--no-progress",
		"--suggest", pairID, fixtureDir,
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("--suggest exited non-zero: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout, stderr.String())
	}
	diff := string(stdout)
	if !strings.Contains(diff, "@@") {
		t.Errorf("stdout missing hunk header. Got:\n%s", diff)
	}
	if !strings.Contains(diff, "extracted_priceWithTaxA_") {
		t.Errorf("stdout missing Java helper signature. Got:\n%s", diff)
	}
	if !strings.Contains(diff, "// Divergences (B vs A):") {
		t.Errorf("stdout missing divergence comment block. Got:\n%s", diff)
	}
}

// TestSuggestAll_JavaSimple_PopulatesSuggestedPatch: --suggest-all
// --json should embed a non-empty unified_diff on every visible pair.
// We assert the pair[0]'s suggested_patch surfaces the helper name and
// that the diff is non-empty (rejection would set Note instead).
func TestSuggestAll_JavaSimple_PopulatesSuggestedPatch(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/refactor/java/simple"

	stdout, err := exec.Command(bin,
		"--threshold", "0.0",
		"--no-cache", "--no-progress",
		"--suggest-all", "--json", fixtureDir,
	).Output()
	if err != nil {
		t.Fatalf("--suggest-all: %v\nstdout:\n%s", err, stdout)
	}
	var doc struct {
		Pairs []struct {
			ID             string `json:"id"`
			SuggestedPatch *struct {
				UnifiedDiff string `json:"unified_diff"`
				HelperName  string `json:"helper_name"`
				Note        string `json:"note"`
			} `json:"suggested_patch"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(stdout, &doc); err != nil {
		t.Fatalf("parse JSON: %v\nstdout:\n%s", err, stdout)
	}
	if len(doc.Pairs) == 0 {
		t.Fatalf("expected at least one pair, got none")
	}
	p := doc.Pairs[0]
	if p.SuggestedPatch == nil {
		t.Fatal("suggested_patch field missing on pair")
	}
	if p.SuggestedPatch.UnifiedDiff == "" {
		t.Errorf("unified_diff empty (note=%q)", p.SuggestedPatch.Note)
	}
	if !strings.HasPrefix(p.SuggestedPatch.HelperName, "extracted_priceWithTaxA_") {
		t.Errorf("helper_name = %q, want extracted_priceWithTaxA_… prefix",
			p.SuggestedPatch.HelperName)
	}
}

// TestSuggest_JavaScriptSimple_ExitsZeroAndPrintsDiff mirrors the Java
// case for the JavaScript emitter.
func TestSuggest_JavaScriptSimple_ExitsZeroAndPrintsDiff(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/refactor/js/simple"

	jsonOut, err := exec.Command(bin,
		"--threshold", "0.0",
		"--no-cache", "--no-progress",
		"--json", fixtureDir,
	).Output()
	if err != nil {
		t.Fatalf("--json discovery: %v\nstdout:\n%s", err, jsonOut)
	}
	var doc struct {
		Pairs []struct {
			ID string `json:"id"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(jsonOut, &doc); err != nil {
		t.Fatalf("parse JSON: %v\nstdout:\n%s", err, jsonOut)
	}
	if len(doc.Pairs) == 0 || doc.Pairs[0].ID == "" {
		t.Fatalf("expected at least one pair with an id, got:\n%s", jsonOut)
	}
	pairID := doc.Pairs[0].ID

	cmd := exec.Command(bin,
		"--no-cache", "--no-progress",
		"--suggest", pairID, fixtureDir,
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("--suggest exited non-zero: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout, stderr.String())
	}
	diff := string(stdout)
	if !strings.Contains(diff, "@@") {
		t.Errorf("stdout missing hunk header. Got:\n%s", diff)
	}
	if !strings.Contains(diff, "extracted_priceWithTaxA_") {
		t.Errorf("stdout missing JS helper signature. Got:\n%s", diff)
	}
	if !strings.Contains(diff, "// Divergences (B vs A):") {
		t.Errorf("stdout missing divergence comment block. Got:\n%s", diff)
	}
}

// TestSuggestAll_JavaScriptSimple_PopulatesSuggestedPatch mirrors the
// Java case: --suggest-all --json embeds a non-empty unified_diff on
// every visible pair.
func TestSuggestAll_JavaScriptSimple_PopulatesSuggestedPatch(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/refactor/js/simple"

	stdout, err := exec.Command(bin,
		"--threshold", "0.0",
		"--no-cache", "--no-progress",
		"--suggest-all", "--json", fixtureDir,
	).Output()
	if err != nil {
		t.Fatalf("--suggest-all: %v\nstdout:\n%s", err, stdout)
	}
	var doc struct {
		Pairs []struct {
			ID             string `json:"id"`
			SuggestedPatch *struct {
				UnifiedDiff string `json:"unified_diff"`
				HelperName  string `json:"helper_name"`
				Note        string `json:"note"`
			} `json:"suggested_patch"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(stdout, &doc); err != nil {
		t.Fatalf("parse JSON: %v\nstdout:\n%s", err, stdout)
	}
	if len(doc.Pairs) == 0 {
		t.Fatalf("expected at least one pair, got none")
	}
	p := doc.Pairs[0]
	if p.SuggestedPatch == nil {
		t.Fatal("suggested_patch field missing on pair")
	}
	if p.SuggestedPatch.UnifiedDiff == "" {
		t.Errorf("unified_diff empty (note=%q)", p.SuggestedPatch.Note)
	}
	if !strings.HasPrefix(p.SuggestedPatch.HelperName, "extracted_priceWithTaxA_") {
		t.Errorf("helper_name = %q, want extracted_priceWithTaxA_… prefix",
			p.SuggestedPatch.HelperName)
	}
}

// TestSuggest_JavaScriptRejectThrow_ExitsNonZeroWithNote: the JS
// reject-throw fixture must produce a note on stderr and exit code 1.
func TestSuggest_JavaScriptRejectThrow_ExitsNonZeroWithNote(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/refactor/js/reject-throw"

	jsonOut, err := exec.Command(bin,
		"--threshold", "0.0",
		"--no-cache", "--no-progress",
		"--json", fixtureDir,
	).Output()
	if err != nil {
		t.Fatalf("--json discovery: %v\nstdout:\n%s", err, jsonOut)
	}
	var doc struct {
		Pairs []struct {
			ID string `json:"id"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(jsonOut, &doc); err != nil {
		t.Fatalf("parse JSON: %v\nstdout:\n%s", err, jsonOut)
	}
	if len(doc.Pairs) == 0 {
		t.Fatalf("expected the reject-throw fixture to surface a pair: %s", jsonOut)
	}
	pairID := doc.Pairs[0].ID

	cmd := exec.Command(bin,
		"--no-cache", "--no-progress",
		"--suggest", pairID, fixtureDir,
	)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err == nil {
		t.Fatalf("expected non-zero exit on rejected pair; stdout:\n%s", stdout.String())
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("expected exit code 1, got err=%v stderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "control-flow asymmetry") {
		t.Errorf("stderr missing rejection note. Got:\n%s", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("expected empty stdout on rejection, got:\n%s", stdout.String())
	}
}

// TestSuggest_RustSimple_ExitsZeroAndPrintsDiff mirrors the Java/JS
// case for the Rust emitter.
func TestSuggest_RustSimple_ExitsZeroAndPrintsDiff(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/refactor/rust/simple"

	jsonOut, err := exec.Command(bin,
		"--threshold", "0.0",
		"--no-cache", "--no-progress",
		"--json", fixtureDir,
	).Output()
	if err != nil {
		t.Fatalf("--json discovery: %v\nstdout:\n%s", err, jsonOut)
	}
	var doc struct {
		Pairs []struct {
			ID string `json:"id"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(jsonOut, &doc); err != nil {
		t.Fatalf("parse JSON: %v\nstdout:\n%s", err, jsonOut)
	}
	if len(doc.Pairs) == 0 || doc.Pairs[0].ID == "" {
		t.Fatalf("expected at least one pair with an id, got:\n%s", jsonOut)
	}
	pairID := doc.Pairs[0].ID

	cmd := exec.Command(bin,
		"--no-cache", "--no-progress",
		"--suggest", pairID, fixtureDir,
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("--suggest exited non-zero: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout, stderr.String())
	}
	diff := string(stdout)
	if !strings.Contains(diff, "@@") {
		t.Errorf("stdout missing hunk header. Got:\n%s", diff)
	}
	if !strings.Contains(diff, "extracted_price_with_tax_a_") {
		t.Errorf("stdout missing Rust helper signature. Got:\n%s", diff)
	}
	if !strings.Contains(diff, "// Divergences (B vs A):") {
		t.Errorf("stdout missing divergence comment block. Got:\n%s", diff)
	}
}

// TestSuggestAll_RustSimple_PopulatesSuggestedPatch mirrors the Java/JS
// case: --suggest-all --json embeds a non-empty unified_diff on every
// visible pair.
func TestSuggestAll_RustSimple_PopulatesSuggestedPatch(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/refactor/rust/simple"

	stdout, err := exec.Command(bin,
		"--threshold", "0.0",
		"--no-cache", "--no-progress",
		"--suggest-all", "--json", fixtureDir,
	).Output()
	if err != nil {
		t.Fatalf("--suggest-all: %v\nstdout:\n%s", err, stdout)
	}
	var doc struct {
		Pairs []struct {
			ID             string `json:"id"`
			SuggestedPatch *struct {
				UnifiedDiff string `json:"unified_diff"`
				HelperName  string `json:"helper_name"`
				Note        string `json:"note"`
			} `json:"suggested_patch"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(stdout, &doc); err != nil {
		t.Fatalf("parse JSON: %v\nstdout:\n%s", err, stdout)
	}
	if len(doc.Pairs) == 0 {
		t.Fatalf("expected at least one pair, got none")
	}
	p := doc.Pairs[0]
	if p.SuggestedPatch == nil {
		t.Fatal("suggested_patch field missing on pair")
	}
	if p.SuggestedPatch.UnifiedDiff == "" {
		t.Errorf("unified_diff empty (note=%q)", p.SuggestedPatch.Note)
	}
	if !strings.HasPrefix(p.SuggestedPatch.HelperName, "extracted_price_with_tax_a_") {
		t.Errorf("helper_name = %q, want extracted_price_with_tax_a_… prefix",
			p.SuggestedPatch.HelperName)
	}
}

// TestSuggest_RustRejectPanic_ExitsNonZeroWithNote: the Rust
// reject-panic fixture must produce a note on stderr and exit code 1.
func TestSuggest_RustRejectPanic_ExitsNonZeroWithNote(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/refactor/rust/reject-panic"

	jsonOut, err := exec.Command(bin,
		"--threshold", "0.0",
		"--no-cache", "--no-progress",
		"--json", fixtureDir,
	).Output()
	if err != nil {
		t.Fatalf("--json discovery: %v\nstdout:\n%s", err, jsonOut)
	}
	var doc struct {
		Pairs []struct {
			ID string `json:"id"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(jsonOut, &doc); err != nil {
		t.Fatalf("parse JSON: %v\nstdout:\n%s", err, jsonOut)
	}
	if len(doc.Pairs) == 0 {
		t.Fatalf("expected the reject-panic fixture to surface a pair: %s", jsonOut)
	}
	pairID := doc.Pairs[0].ID

	cmd := exec.Command(bin,
		"--no-cache", "--no-progress",
		"--suggest", pairID, fixtureDir,
	)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err == nil {
		t.Fatalf("expected non-zero exit on rejected pair; stdout:\n%s", stdout.String())
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("expected exit code 1, got err=%v stderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "control-flow asymmetry") {
		t.Errorf("stderr missing rejection note. Got:\n%s", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("expected empty stdout on rejection, got:\n%s", stdout.String())
	}
}

// TestSuggest_ElixirSimple_ExitsZeroAndPrintsDiff mirrors the Java/JS/Rust
// case for the Elixir emitter.
func TestSuggest_ElixirSimple_ExitsZeroAndPrintsDiff(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/refactor/elixir/simple"

	jsonOut, err := exec.Command(bin,
		"--threshold", "0.0",
		"--no-cache", "--no-progress",
		"--json", fixtureDir,
	).Output()
	if err != nil {
		t.Fatalf("--json discovery: %v\nstdout:\n%s", err, jsonOut)
	}
	var doc struct {
		Pairs []struct {
			ID string `json:"id"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(jsonOut, &doc); err != nil {
		t.Fatalf("parse JSON: %v\nstdout:\n%s", err, jsonOut)
	}
	if len(doc.Pairs) == 0 || doc.Pairs[0].ID == "" {
		t.Fatalf("expected at least one pair with an id, got:\n%s", jsonOut)
	}
	pairID := doc.Pairs[0].ID

	cmd := exec.Command(bin,
		"--no-cache", "--no-progress",
		"--suggest", pairID, fixtureDir,
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("--suggest exited non-zero: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout, stderr.String())
	}
	diff := string(stdout)
	if !strings.Contains(diff, "@@") {
		t.Errorf("stdout missing hunk header. Got:\n%s", diff)
	}
	if !strings.Contains(diff, "extracted_price_with_tax_") {
		t.Errorf("stdout missing Elixir helper signature. Got:\n%s", diff)
	}
	if !strings.Contains(diff, "# Divergences (B vs A):") {
		t.Errorf("stdout missing divergence comment block. Got:\n%s", diff)
	}
	if !strings.Contains(diff, "# NOTE:") {
		t.Errorf("stdout missing the module-context NOTE. Got:\n%s", diff)
	}
}

// TestSuggestAll_ElixirSimple_PopulatesSuggestedPatch mirrors the
// Java/JS/Rust case: --suggest-all --json embeds a non-empty
// unified_diff on every visible pair.
func TestSuggestAll_ElixirSimple_PopulatesSuggestedPatch(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/refactor/elixir/simple"

	stdout, err := exec.Command(bin,
		"--threshold", "0.0",
		"--no-cache", "--no-progress",
		"--suggest-all", "--json", fixtureDir,
	).Output()
	if err != nil {
		t.Fatalf("--suggest-all: %v\nstdout:\n%s", err, stdout)
	}
	var doc struct {
		Pairs []struct {
			ID             string `json:"id"`
			SuggestedPatch *struct {
				UnifiedDiff string `json:"unified_diff"`
				HelperName  string `json:"helper_name"`
				Note        string `json:"note"`
			} `json:"suggested_patch"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(stdout, &doc); err != nil {
		t.Fatalf("parse JSON: %v\nstdout:\n%s", err, stdout)
	}
	if len(doc.Pairs) == 0 {
		t.Fatalf("expected at least one pair, got none")
	}
	p := doc.Pairs[0]
	if p.SuggestedPatch == nil {
		t.Fatal("suggested_patch field missing on pair")
	}
	if p.SuggestedPatch.UnifiedDiff == "" {
		t.Errorf("unified_diff empty (note=%q)", p.SuggestedPatch.Note)
	}
	if !strings.HasPrefix(p.SuggestedPatch.HelperName, "extracted_price_with_tax_") {
		t.Errorf("helper_name = %q, want extracted_price_with_tax_… prefix",
			p.SuggestedPatch.HelperName)
	}
}

// TestSuggest_ElixirRejectRaise_ExitsNonZeroWithNote: the Elixir
// reject-raise fixture must produce a note on stderr and exit code 1.
func TestSuggest_ElixirRejectRaise_ExitsNonZeroWithNote(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/refactor/elixir/reject-raise"

	jsonOut, err := exec.Command(bin,
		"--threshold", "0.0",
		"--no-cache", "--no-progress",
		"--json", fixtureDir,
	).Output()
	if err != nil {
		t.Fatalf("--json discovery: %v\nstdout:\n%s", err, jsonOut)
	}
	var doc struct {
		Pairs []struct {
			ID string `json:"id"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(jsonOut, &doc); err != nil {
		t.Fatalf("parse JSON: %v\nstdout:\n%s", err, jsonOut)
	}
	if len(doc.Pairs) == 0 {
		t.Fatalf("expected the reject-raise fixture to surface a pair: %s", jsonOut)
	}
	pairID := doc.Pairs[0].ID

	cmd := exec.Command(bin,
		"--no-cache", "--no-progress",
		"--suggest", pairID, fixtureDir,
	)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err == nil {
		t.Fatalf("expected non-zero exit on rejected pair; stdout:\n%s", stdout.String())
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("expected exit code 1, got err=%v stderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "control-flow asymmetry") {
		t.Errorf("stderr missing rejection note. Got:\n%s", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("expected empty stdout on rejection, got:\n%s", stdout.String())
	}
}

// TestSuggest_ElixirRealworldGenServer_ExitsZeroAndPrintsDiff: end-to-end
// regression test for v2 Elixir capabilities. The realworld-genserver
// fixture exercises @impl attributes, do: shorthand (`init`, `lookup`),
// pattern-matched arg shapes, multi-line block-form callbacks
// (`handle_call`, `handle_cast`), and nested `case`/`do` blocks. We
// pick the `handle_cast` pair (which has a divergent Logger string)
// and confirm --suggest produces a sensible helper.
func TestSuggest_ElixirRealworldGenServer_ExitsZeroAndPrintsDiff(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/refactor/elixir/realworld-genserver"

	jsonOut, err := exec.Command(bin,
		"--threshold", "0.0",
		"--no-cache", "--no-progress",
		"--json", fixtureDir,
	).Output()
	if err != nil {
		t.Fatalf("--json discovery: %v\nstdout:\n%s", err, jsonOut)
	}
	var doc struct {
		Pairs []struct {
			ID    string `json:"id"`
			FileA string `json:"file_a"`
			FileB string `json:"file_b"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(jsonOut, &doc); err != nil {
		t.Fatalf("parse JSON: %v\nstdout:\n%s", err, jsonOut)
	}
	if len(doc.Pairs) == 0 {
		t.Fatalf("expected at least one pair on the realworld-genserver fixture; got:\n%s", jsonOut)
	}
	// Find the handle_cast pair (the one with the Logger divergence).
	pairID := ""
	for _, p := range doc.Pairs {
		if strings.Contains(p.FileA, "handle_cast") && strings.Contains(p.FileB, "handle_cast") {
			pairID = p.ID
			break
		}
	}
	if pairID == "" {
		t.Fatalf("expected a handle_cast/handle_cast pair; got:\n%s", jsonOut)
	}

	cmd := exec.Command(bin,
		"--no-cache", "--no-progress",
		"--suggest", pairID, fixtureDir,
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("--suggest exited non-zero: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout, stderr.String())
	}
	diff := string(stdout)
	if !strings.Contains(diff, "@@") {
		t.Errorf("stdout missing hunk header. Got:\n%s", diff)
	}
	if !strings.Contains(diff, "extracted_") {
		t.Errorf("stdout missing extracted_… helper. Got:\n%s", diff)
	}
	if !strings.Contains(diff, "# NOTE:") {
		t.Errorf("stdout missing the module-context NOTE. Got:\n%s", diff)
	}
}

// TestSuggest_JavaRejectThrow_ExitsNonZeroWithNote: the reject-throw
// fixture must produce a note on stderr and exit code 1, NOT a diff on
// stdout. This guards the rejection-routing contract documented in
// emitSuggestion.
func TestSuggest_JavaRejectThrow_ExitsNonZeroWithNote(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/refactor/java/reject-throw"

	jsonOut, err := exec.Command(bin,
		"--threshold", "0.0",
		"--no-cache", "--no-progress",
		"--json", fixtureDir,
	).Output()
	if err != nil {
		t.Fatalf("--json discovery: %v\nstdout:\n%s", err, jsonOut)
	}
	var doc struct {
		Pairs []struct {
			ID string `json:"id"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(jsonOut, &doc); err != nil {
		t.Fatalf("parse JSON: %v\nstdout:\n%s", err, jsonOut)
	}
	if len(doc.Pairs) == 0 {
		t.Fatalf("expected the reject-throw fixture to surface a pair: %s", jsonOut)
	}
	pairID := doc.Pairs[0].ID

	cmd := exec.Command(bin,
		"--no-cache", "--no-progress",
		"--suggest", pairID, fixtureDir,
	)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err == nil {
		t.Fatalf("expected non-zero exit on rejected pair; stdout:\n%s", stdout.String())
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("expected exit code 1, got err=%v stderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "control-flow asymmetry") {
		t.Errorf("stderr missing rejection note. Got:\n%s", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("expected empty stdout on rejection, got:\n%s", stdout.String())
	}
}

// TestSuggest_TypeScriptRealworld_ExitsZeroAndPrintsAnnotatedHelper:
// discover the class-method pair (loadA/loadB) of the .ts fixture via
// --json, then invoke --suggest <id>. Asserts exit 0 and that the diff
// carries the TS parameter and return-type annotations on the helper
// header (with the `private` access modifier dropped). Also pins that
// the pipeline treats .ts files as JavaScript end-to-end. Bet #4
// deferred follow-up.
func TestSuggest_TypeScriptRealworld_ExitsZeroAndPrintsAnnotatedHelper(t *testing.T) {
	bin := subprocessBin(t)
	fixtureDir := "../../testdata/refactor/js/realworld-typescript"

	jsonOut, err := exec.Command(bin,
		"--threshold", "0.0",
		"--no-cache", "--no-progress",
		"--json", fixtureDir,
	).Output()
	if err != nil {
		t.Fatalf("--json discovery: %v\nstdout:\n%s", err, jsonOut)
	}
	var doc struct {
		Pairs []struct {
			ID    string `json:"id"`
			FileA string `json:"file_a"`
			FileB string `json:"file_b"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal(jsonOut, &doc); err != nil {
		t.Fatalf("parse JSON: %v\nstdout:\n%s", err, jsonOut)
	}
	pairID := ""
	for _, p := range doc.Pairs {
		if strings.Contains(p.FileA, "load") && strings.Contains(p.FileB, "load") {
			pairID = p.ID
			break
		}
	}
	if pairID == "" {
		t.Fatalf("no loadA/loadB pair found in JSON output:\n%s", jsonOut)
	}

	cmd := exec.Command(bin,
		"--no-cache", "--no-progress",
		"--suggest", pairID, fixtureDir,
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("--suggest exited non-zero: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout, stderr.String())
	}
	diff := string(stdout)
	if !strings.Contains(diff, "@@") {
		t.Errorf("stdout missing hunk header. Got:\n%s", diff)
	}
	if !strings.Contains(diff, "async function extracted_load") {
		t.Errorf("stdout missing annotated TS helper header. Got:\n%s", diff)
	}
	if !strings.Contains(diff, ": Promise<Widget>") {
		t.Errorf("stdout missing return-type annotation on helper. Got:\n%s", diff)
	}
	if !strings.Contains(diff, "// Divergences (B vs A):") {
		t.Errorf("stdout missing divergence comment block. Got:\n%s", diff)
	}
}
