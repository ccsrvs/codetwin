package splitter

import (
	"reflect"
	"testing"
)

func matchAlwaysFalse(string) (string, bool) { return "", false }

func matchKeyword(keyword, symbol string) braceMatcher {
	return func(line string) (string, bool) {
		if line == keyword {
			return symbol, true
		}
		return "", false
	}
}

func TestSplitBraceBased_Given_NoMatches_When_Split_Then_ReturnsNoChunks(t *testing.T) {
	code := "alpha\nbeta\ngamma\n"

	chunks := splitBraceBased(code, matchAlwaysFalse, false)

	if len(chunks) != 0 {
		t.Errorf("expected no chunks, got %d", len(chunks))
	}
}

func TestSplitBraceBased_Given_EmptyInput_When_Split_Then_ReturnsNoChunks(t *testing.T) {
	chunks := splitBraceBased("", matchAlwaysFalse, false)

	if len(chunks) != 0 {
		t.Errorf("expected no chunks, got %d", len(chunks))
	}
}

func TestSplitBraceBased_Given_SingleBracedDefinition_When_Split_Then_EmitsChunkWithFullBody(t *testing.T) {
	code := "header {\n  body\n}\n"
	match := matchKeyword("header {", "Run")

	chunks := splitBraceBased(code, match, false)

	want := []Chunk{{StartLine: 1, EndLine: 3, Symbol: "Run", Code: "header {\n  body\n}"}}
	if !reflect.DeepEqual(chunks, want) {
		t.Errorf("got %+v, want %+v", chunks, want)
	}
}

func TestSplitBraceBased_Given_BodylessHeader_When_EmitBodylessFalse_Then_NoChunk(t *testing.T) {
	code := "stub_header\nunrelated\n"
	match := matchKeyword("stub_header", "Stub")

	chunks := splitBraceBased(code, match, false)

	if len(chunks) != 0 {
		t.Errorf("expected no chunks for bodyless header with emitBodyless=false, got %d: %+v", len(chunks), chunks)
	}
}

func TestSplitBraceBased_Given_BodylessHeader_When_EmitBodylessTrue_Then_EmitsSingleLineChunk(t *testing.T) {
	code := "arrow_header\nunrelated\n"
	match := matchKeyword("arrow_header", "Lambda")

	chunks := splitBraceBased(code, match, true)

	want := []Chunk{{StartLine: 1, EndLine: 1, Symbol: "Lambda", Code: "arrow_header"}}
	if !reflect.DeepEqual(chunks, want) {
		t.Errorf("got %+v, want %+v", chunks, want)
	}
}

func TestSplitBraceBased_Given_TwoSiblingDefinitions_When_Split_Then_EmitsBothInOrder(t *testing.T) {
	code := "h1 {\n  body1\n}\n" +
		"between\n" +
		"h2 {\n  body2\n}\n"
	match := func(line string) (string, bool) {
		switch line {
		case "h1 {":
			return "First", true
		case "h2 {":
			return "Second", true
		}
		return "", false
	}

	chunks := splitBraceBased(code, match, false)

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Symbol != "First" || chunks[0].StartLine != 1 || chunks[0].EndLine != 3 {
		t.Errorf("chunk[0] = %+v; want First @ 1-3", chunks[0])
	}
	if chunks[1].Symbol != "Second" || chunks[1].StartLine != 5 || chunks[1].EndLine != 7 {
		t.Errorf("chunk[1] = %+v; want Second @ 5-7", chunks[1])
	}
}

func TestSplitBraceBased_Given_MatcherWouldMatchInsideBody_When_Split_Then_SkipsNestedMatch(t *testing.T) {
	// After emitting an outer chunk, i must jump to end+1 so we don't
	// re-match definitions buried inside the body.
	code := "outer {\n  inner {\n    body\n  }\n}\n"
	match := func(line string) (string, bool) {
		switch line {
		case "outer {":
			return "Outer", true
		case "  inner {":
			return "Inner", true
		}
		return "", false
	}

	chunks := splitBraceBased(code, match, false)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk (nested match must be skipped), got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Symbol != "Outer" || chunks[0].StartLine != 1 || chunks[0].EndLine != 5 {
		t.Errorf("chunk = %+v; want Outer @ 1-5", chunks[0])
	}
}

func TestSplitBraceBased_Given_HeaderWithFollowingDefinitionAfterBody_When_Split_Then_EmitsBoth(t *testing.T) {
	// After emitting a chunk, the loop must continue scanning past the body's
	// closing brace and find the next sibling definition.
	code := "h1 {\n  inner\n}\nh2 {\n  inner2\n}\n"
	match := func(line string) (string, bool) {
		if line == "h1 {" {
			return "A", true
		}
		if line == "h2 {" {
			return "B", true
		}
		return "", false
	}

	chunks := splitBraceBased(code, match, false)

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].EndLine != 3 || chunks[1].StartLine != 4 {
		t.Errorf("chunk boundaries wrong: %+v", chunks)
	}
}
