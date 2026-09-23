package tokenizer

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestNormalize_StripsCIncludes(t *testing.T) {
	withIncludes := "#include <stdio.h>\n#  include \"local.h\"\n#include<stdint.h>\nint f(void) { return 1; }\n"
	plain := "int f(void) { return 1; }\n"
	if got, want := Normalize(withIncludes, C), Normalize(plain, C); got != want {
		t.Errorf("Normalize with includes = %q, want %q", got, want)
	}
}

func TestTokenize_CNumberLiteralsBecomeNUM(t *testing.T) {
	tokens := Tokenize("x = 0x1Fu + 0XffUL + 10UL + 1.5f + 2e-3 + 017 + 3. + 0x1p3 + 0x1.8p-2f + 0b1010u + 10uz + .5f;", C)
	for _, tok := range tokens {
		if tok != "NUM" && strings.ContainsAny(tok, "0123456789") {
			t.Errorf("raw numeric token %q survived in %v", tok, tokens)
		}
	}
	if n := strings.Count(strings.Join(tokens, " "), "NUM"); n != 12 {
		t.Errorf("got %d NUM tokens, want 12: %v", n, tokens)
	}
	for _, tok := range tokens {
		if tok == "VAR" {
			continue
		}
		if len(tok) > 1 && (tok[0] == 'p' || tok[0] == 'b' || tok[0] == 'x') {
			t.Errorf("literal split into a trailing word %q: %v", tok, tokens)
		}
	}
	if n := strings.Count(strings.Join(tokens, " "), "VAR"); n != 1 {
		t.Errorf("only x may normalize to VAR, got %d VAR tokens: %v", n, tokens)
	}
}

func TestTokenize_CKeywordsKeptIdentifiersNormalized(t *testing.T) {
	tokens := Tokenize("static unsigned int count_items(const struct list *head) { return sizeof(*head); }", C)
	for _, kw := range []string{"static", "unsigned", "int", "const", "struct", "return", "sizeof"} {
		if !slices.Contains(tokens, kw) {
			t.Errorf("keyword %q normalized away: %v", kw, tokens)
		}
	}
	for _, ident := range []string{"count_items", "list", "head"} {
		if slices.Contains(tokens, ident) {
			t.Errorf("identifier %q not normalized to VAR: %v", ident, tokens)
		}
	}
}

// Renamed C clones must tokenize identically: identifiers, string and
// number literals all normalize.
func TestTokenize_CRenamedCloneTokenizesIdentically(t *testing.T) {
	a := "int sum_even(const int *xs, size_t n) {\n\tint total = 0;\n\tfor (size_t i = 0; i < n; i++) {\n\t\tif (xs[i] % 2 == 0) total += xs[i];\n\t}\n\treturn total;\n}\n"
	b := "int add_evens(const int *vals, size_t len) {\n\tint acc = 0;\n\tfor (size_t k = 0; k < len; k++) {\n\t\tif (vals[k] % 2 == 0) acc += vals[k];\n\t}\n\treturn acc;\n}\n"
	if ta, tb := Tokenize(a, C), Tokenize(b, C); !reflect.DeepEqual(ta, tb) {
		t.Errorf("renamed clone tokens differ:\n%v\n%v", ta, tb)
	}
}

func TestReferences_CStripsCommentsKeepsStrings(t *testing.T) {
	code := "/* calls oldHelper */\nint caller(void) {\n\tlookup(\"dynamicName\"); // not staleHelper\n\treturn realHelper();\n}\n"
	refs := refWords(References(code, C))
	if len(refs["oldHelper"]) != 0 || len(refs["staleHelper"]) != 0 {
		t.Errorf("comment words produced references: %v", refs)
	}
	if !reflect.DeepEqual(refs["realHelper"], []int{4}) || !reflect.DeepEqual(refs["dynamicName"], []int{3}) {
		t.Errorf("references = %v, want realHelper on 4 and dynamicName on 3", refs)
	}
}

func TestLexicalTerms_CSkipsKeywordsAndIncludes(t *testing.T) {
	terms := LexicalTerms("#include <socket_lib.h>\nstatic int open_socket(const char *host_name) {\n\treturn connect_to(host_name, \"retry later\");\n}\n", C)
	for _, want := range []string{"open", "socket", "host", "name", "connect", "retry", "later"} {
		if !slices.Contains(terms, want) {
			t.Errorf("term %q missing from %v", want, terms)
		}
	}
	for _, unwanted := range []string{"static", "int", "const", "char", "return", "lib"} {
		if slices.Contains(terms, unwanted) {
			t.Errorf("keyword or include term %q leaked into %v", unwanted, terms)
		}
	}
}

// Every preprocessor line is configuration, not code, for similarity:
// conditional-compilation lines, #define constants, and continued macro
// bodies are stripped (as PMD CPD does), so two functions that differ
// only in their #ifdef scaffolding still match and a file of macro
// boilerplate has little code left. References still see directives.
func TestTokenize_CStripsAllPreprocessorLines(t *testing.T) {
	withPP := "#define SYMBOL_NAME cosh4\n#include \"ifunc.h\"\nint f(int x)\n{\n#ifdef FAST\n\tx <<= 1;\n#else\n\tx *= 2;\n#endif\n#define LONG_MACRO(a) \\\n\tdo { a; } while (0)\n\treturn x;\n}\n"
	plain := "int f(int x)\n{\n\tx <<= 1;\n\tx *= 2;\n\treturn x;\n}\n"
	got, lines := TokenizeWithLines(withPP, C)
	want := Tokenize(plain, C)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tokens = %v\nwant     %v", got, want)
	}
	if lines[0] != 3 || lines[len(lines)-1] != 13 {
		t.Errorf("line numbers must still point at the source: %v", lines)
	}
	refs := refWords(References("#define CALL_HELPER() helper()\nint g(void) { return 0; }\n", C))
	if len(refs["helper"]) != 1 {
		t.Errorf("a #define body must still count as a reference: %v", refs)
	}
}
