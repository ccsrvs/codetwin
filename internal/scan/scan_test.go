package scan

import (
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"

	"github.com/ccsrvs/codetwin/internal/cache"
	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func TestProcessFile_GivenValidJSFile_When_Process_Then_ReturnsSnippetsWithExpectedFields(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "sum.js", "function sumArray(arr) {\n  let total = 0;\n  for (let i = 0; i < arr.length; i++) {\n    total += arr[i];\n  }\n  return total;\n}\n")

	cacheState := cache.New()
	snips, warning := ProcessFile(path, 1, nil, cacheState, "")

	if warning != "" {
		t.Fatalf("unexpected warning: %s", warning)
	}
	if len(snips) != 1 {
		t.Fatalf("expected 1 snippet, got %d", len(snips))
	}
	s := snips[0]
	if s.Lang != tokenizer.JavaScript {
		t.Errorf("Lang = %v, want JavaScript", s.Lang)
	}
	if s.Name == "" {
		t.Errorf("Name is empty")
	}
	if len(s.Tokens) == 0 {
		t.Errorf("Tokens is empty")
	}
	if s.NonBlankLn <= 0 {
		t.Errorf("NonBlankLn = %d, want > 0", s.NonBlankLn)
	}
	if s.Fps.K == 0 {
		t.Errorf("Fps.K is zero — fingerprint not built")
	}
}

func TestProcessFile_GivenUnreadablePath_When_Process_Then_ReturnsWarning(t *testing.T) {
	cacheState := cache.New()
	snips, warning := ProcessFile("/nonexistent/file.js", 1, nil, cacheState, "")

	if len(snips) != 0 {
		t.Errorf("expected no snippets on read error, got %d", len(snips))
	}
	if warning == "" {
		t.Errorf("expected non-empty warning string")
	}
}

func TestProcessFile_GivenChunkBelowMinLines_When_Process_Then_DoesNotReturnIt(t *testing.T) {
	dir := t.TempDir()
	// Single-line function: NonBlankLn will be small.
	path := writeFile(t, dir, "tiny.js", "function tiny(x) { return x; }\n")
	cacheState := cache.New()

	snips, warning := ProcessFile(path, 100, nil, cacheState, "")

	if warning != "" {
		t.Fatalf("unexpected warning: %s", warning)
	}
	if len(snips) != 0 {
		t.Errorf("expected 0 snippets when minLines=100, got %d", len(snips))
	}
}

func TestProcessFile_GivenSecondCall_When_CacheWarm_Then_ReturnsEquivalentSnippets(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "sum.js", "function sumArray(arr) {\n  let total = 0;\n  for (let i = 0; i < arr.length; i++) {\n    total += arr[i];\n  }\n  return total;\n}\n")
	cacheState := cache.New()

	first, _ := ProcessFile(path, 1, nil, cacheState, "")
	second, _ := ProcessFile(path, 1, nil, cacheState, "")

	if len(first) != len(second) {
		t.Fatalf("snippet count diverged across calls: first=%d second=%d", len(first), len(second))
	}
	for i := range first {
		if first[i].Name != second[i].Name {
			t.Errorf("snippet %d: Name differs (first=%s second=%s)", i, first[i].Name, second[i].Name)
		}
		if first[i].NonBlankLn != second[i].NonBlankLn {
			t.Errorf("snippet %d: NonBlankLn differs", i)
		}
	}
}

func TestProcessFiles_GivenMultipleFiles_When_Process_Then_ReturnsAllSnippets(t *testing.T) {
	dir := t.TempDir()
	body := "function fn(arr) {\n  let total = 0;\n  for (let i = 0; i < arr.length; i++) {\n    total += arr[i];\n  }\n  return total;\n}\n"
	files := []string{
		writeFile(t, dir, "a.js", body),
		writeFile(t, dir, "b.js", body),
		writeFile(t, dir, "c.js", body),
	}
	cacheState := cache.New()

	snips, warnings := ProcessFiles(files, 1, nil, cacheState, "", nil)

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if len(snips) != 3 {
		t.Fatalf("expected 3 snippets, got %d", len(snips))
	}
	// Worker goroutines complete in nondeterministic order — sort by name
	// so the assertion is order-independent.
	names := make([]string, len(snips))
	for i, s := range snips {
		names[i] = s.Name
	}
	sort.Strings(names)
	for _, n := range names {
		if n == "" {
			t.Errorf("snippet has empty name")
		}
	}
}

func TestProcessFiles_GivenOnFileDoneCallback_When_Process_Then_FiresOncePerFile(t *testing.T) {
	dir := t.TempDir()
	body := "function fn(x) {\n  return x;\n}\n"
	files := []string{
		writeFile(t, dir, "a.js", body),
		writeFile(t, dir, "b.js", body),
		writeFile(t, dir, "c.js", body),
		writeFile(t, dir, "d.js", body),
	}
	cacheState := cache.New()
	var calls atomic.Int64

	_, _ = ProcessFiles(files, 1, nil, cacheState, "", func() { calls.Add(1) })

	if got := calls.Load(); got != 4 {
		t.Errorf("onFileDone fired %d times, want 4", got)
	}
}

func TestProcessFiles_GivenEmptyFileList_When_Process_Then_ReturnsNothing(t *testing.T) {
	cacheState := cache.New()
	snips, warnings := ProcessFiles(nil, 1, nil, cacheState, "", nil)

	if snips != nil {
		t.Errorf("expected nil snippets, got %v", snips)
	}
	if warnings != nil {
		t.Errorf("expected nil warnings, got %v", warnings)
	}
}
