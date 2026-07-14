package tokenizer

import (
	"reflect"
	"testing"
)

func TestLexicalTerms_GoIdentifiersAndStrings(t *testing.T) {
	code := `func withdraw(balance float64) error {
	// debit the account
	if balance < minBalance {
		return errors.New("insufficient funds")
	}
	return nil
}`
	got := LexicalTerms(code, Go)
	// "new" is in Go's keyword set (skipped even as errors.New);
	// "error" is an ordinary identifier. Comment words never leak.
	want := []string{"balance", "error", "errors", "float", "funds", "insufficient", "min", "withdraw"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LexicalTerms = %v, want %v", got, want)
	}
}

func TestLexicalTerms_SkipsKeywordsCommentsAndShortTerms(t *testing.T) {
	code := `for i := range xs {
	// this comment vocabulary must not leak
	go f(i)
}`
	got := LexicalTerms(code, Go)
	// "for", "range", "go" are keywords; "i", "f" are single-rune;
	// only "xs" survives.
	want := []string{"xs"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LexicalTerms = %v, want %v", got, want)
	}
}

func TestLexicalTerms_CamelSnakeAndAcronymSplitting(t *testing.T) {
	code := `parseHTTPResponse(user_id, maxRetries2, "readTimeout")`
	got := LexicalTerms(code, Go)
	want := []string{"http", "id", "max", "parse", "read", "response", "retries", "timeout", "user"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LexicalTerms = %v, want %v", got, want)
	}
}

func TestLexicalTerms_PureNumbersDropped(t *testing.T) {
	code := `limit := 12345 + offset42`
	got := LexicalTerms(code, Go)
	// "12345" is pure digits (dropped); "offset42" splits into
	// "offset" + "42" (digits dropped).
	want := []string{"limit", "offset"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LexicalTerms = %v, want %v", got, want)
	}
}

func TestLexicalTerms_StringContentsHarvested(t *testing.T) {
	code := `raise ValueError("retries capped at ten")`
	got := LexicalTerms(code, Python)
	want := []string{"at", "capped", "error", "retries", "ten", "value"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LexicalTerms = %v, want %v", got, want)
	}
}

func TestLexicalTerms_EmptyForKeywordOnlyCode(t *testing.T) {
	if got := LexicalTerms("if else for", Go); got != nil {
		t.Errorf("LexicalTerms(keyword-only) = %v, want nil", got)
	}
}

func TestLexicalTerms_RenameInvarianceIsPartial(t *testing.T) {
	// The core property the structural-twin gate relies on: a typical
	// rename keeps most vocabulary (fields, helpers, strings) so its
	// term sets overlap; disjoint-vocabulary twins share ~nothing.
	a := LexicalTerms(`total += line.UnitPrice`, Go)
	b := LexicalTerms(`sum += item.UnitPrice`, Go)
	shared := 0
	set := map[string]bool{}
	for _, x := range a {
		set[x] = true
	}
	for _, x := range b {
		if set[x] {
			shared++
		}
	}
	if shared < 2 { // "unit", "price"
		t.Errorf("expected renamed code to share field vocabulary, got a=%v b=%v", a, b)
	}
}
