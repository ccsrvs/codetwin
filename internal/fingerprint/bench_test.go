package fingerprint

import (
	"os"
	"testing"

	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

// benchTokens tokenizes a real Go source file from this repo so the
// benchmark's token length/character distribution matches what the
// scanner actually feeds GeneratePositional (short punctuation tokens
// interleaved with VAR/keyword tokens), rather than synthetic uniform
// tokens.
func benchTokens(b *testing.B) []string {
	b.Helper()
	const src = "../similarity/matrix.go"
	data, err := os.ReadFile(src)
	if err != nil {
		b.Fatalf("read %s: %v", src, err)
	}
	tokens := tokenizer.Tokenize(string(data), tokenizer.Go)
	if len(tokens) < 10*DefaultK {
		b.Fatalf("unexpectedly short token stream (%d tokens) from %s", len(tokens), src)
	}
	return tokens
}

func BenchmarkGeneratePositional(b *testing.B) {
	tokens := benchTokens(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GeneratePositional(tokens, DefaultK, DefaultW)
	}
}

func BenchmarkGenerate(b *testing.B) {
	tokens := benchTokens(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Generate(tokens, DefaultK, DefaultW)
	}
}
