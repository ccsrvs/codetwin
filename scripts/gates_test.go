package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGate(t *testing.T, script, input string, args ...string) (string, error) {
	t.Helper()

	inputPath := filepath.Join(t.TempDir(), "input.txt")
	if err := os.WriteFile(inputPath, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}

	commandArgs := append([]string{script, inputPath}, args...)
	cmd := exec.Command("bash", commandArgs...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestCoverageGate(t *testing.T) {
	tests := []struct {
		name       string
		report     string
		minimum    string
		wantErr    bool
		wantOutput string
	}{
		{
			name:       "passes at or above floor",
			report:     "github.com/example/project/a.go:1:\tf\t100.0%\ntotal:\t(statements)\t81.1%\n",
			minimum:    "80.0",
			wantOutput: "81.1% meets 80.0% minimum",
		},
		{
			name:       "fails below floor",
			report:     "total:\t(statements)\t79.9%\n",
			minimum:    "80.0",
			wantErr:    true,
			wantOutput: "79.9% is below 80.0% minimum",
		},
		{
			name:       "rejects missing total",
			report:     "github.com/example/project/a.go:1:\tf\t100.0%\n",
			minimum:    "80.0",
			wantErr:    true,
			wantOutput: "total coverage not found",
		},
		{
			name:       "rejects malformed total",
			report:     "total:\t(statements)\tunknown\n",
			minimum:    "80.0",
			wantErr:    true,
			wantOutput: "invalid total coverage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := runGate(t, "check-coverage.sh", tt.report, tt.minimum)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v; output:\n%s", err, tt.wantErr, output)
			}
			if !strings.Contains(output, tt.wantOutput) {
				t.Fatalf("output %q does not contain %q", output, tt.wantOutput)
			}
		})
	}
}

func TestPerformanceGate(t *testing.T) {
	const good = `BenchmarkSimilarityStoragePipeline/dense-8 3 100000000 ns/op 20000000 B/op 100 allocs/op
BenchmarkSimilarityStoragePipeline/dense-8 3 102000000 ns/op 20200000 B/op 100 allocs/op
BenchmarkSimilarityStoragePipeline/sparse-8 3 50000000 ns/op 15000000 B/op 80 allocs/op
BenchmarkSimilarityStoragePipeline/sparse-8 3 52000000 ns/op 15200000 B/op 80 allocs/op
BenchmarkSimilarityStoragePipeline/incremental-cold-8 3 55000000 ns/op 20500000 B/op 90 allocs/op
BenchmarkSimilarityStoragePipeline/incremental-cold-8 3 56000000 ns/op 20700000 B/op 90 allocs/op
BenchmarkSimilarityStoragePipeline/incremental-warm-8 3 45000000 ns/op 14500000 B/op 70 allocs/op
BenchmarkSimilarityStoragePipeline/incremental-warm-8 3 46000000 ns/op 14700000 B/op 70 allocs/op
`

	tests := []struct {
		name       string
		report     string
		wantErr    bool
		wantOutput string
	}{
		{name: "passes healthy relative performance", report: good, wantOutput: "performance gate passed"},
		{
			name:       "fails sparse runtime regression",
			report:     strings.ReplaceAll(strings.ReplaceAll(good, "50000000 ns/op", "140000000 ns/op"), "52000000 ns/op", "142000000 ns/op"),
			wantErr:    true,
			wantOutput: "sparse time ratio",
		},
		{
			name:       "fails sparse memory regression",
			report:     strings.ReplaceAll(good, "15000000 B/op", "19000000 B/op"),
			wantErr:    true,
			wantOutput: "sparse memory ratio",
		},
		{
			// GOMAXPROCS=1 runners print benchmark names with no -N suffix.
			name:       "accepts single-CPU benchmark names",
			report:     strings.ReplaceAll(good, "-8 3 ", " 3 "),
			wantOutput: "performance gate passed",
		},
		{
			name:       "rejects incomplete benchmark output",
			report:     strings.ReplaceAll(good, "BenchmarkSimilarityStoragePipeline/incremental-warm", "BenchmarkOther/incremental-warm"),
			wantErr:    true,
			wantOutput: "missing benchmark metrics",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := runGate(t, "check-performance.sh", tt.report)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v; output:\n%s", err, tt.wantErr, output)
			}
			if !strings.Contains(output, tt.wantOutput) {
				t.Fatalf("output %q does not contain %q", output, tt.wantOutput)
			}
		})
	}
}
