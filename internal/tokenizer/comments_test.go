package tokenizer

import (
	"reflect"
	"slices"
	"testing"
)

func TestCommentMarkersInsideStrings(t *testing.T) {
	for _, tc := range []struct {
		lang             Language
		literal, comment string
	}{
		{Go, `"https://aliveHelper"`, "// ignoredHelper"},
		{JavaScript, `"https://aliveHelper"`, "// ignoredHelper"},
		{Java, `"/* aliveHelper */"`, "// ignoredHelper"},
		{Rust, `"https://aliveHelper"`, "// ignoredHelper"},
		{Python, `"#aliveHelper"`, "# ignoredHelper"},
		{Elixir, `"#aliveHelper"`, "# ignoredHelper"},
	} {
		t.Run(string(tc.lang), func(t *testing.T) {
			source := "call(" + tc.literal + "); afterHelper() " + tc.comment + "\nnextHelper()"
			plain := "call(\"value\"); afterHelper()\nnextHelper()"
			got, lines := TokenizeWithLines(source, tc.lang)
			want, wantLines := TokenizeWithLines(plain, tc.lang)
			if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(lines, wantLines) {
				t.Errorf("tokens/lines = %v / %v, want %v / %v", got, lines, want, wantLines)
			}
			if got, want := Normalize(source, tc.lang), Normalize(plain, tc.lang); got != want {
				t.Errorf("normalized = %q, want %q", got, want)
			}
			refs := refWords(References(source, tc.lang))
			for _, name := range []string{"aliveHelper", "afterHelper"} {
				if !reflect.DeepEqual(refs[name], []int{1}) {
					t.Errorf("references to %s = %v, want [1]", name, refs[name])
				}
			}
			if len(refs["ignoredHelper"]) != 0 {
				t.Error("comment must not produce references")
			}
			if terms := LexicalTerms(source, tc.lang); !slices.Contains(terms, "alive") {
				t.Errorf("string vocabulary missing: %v", terms)
			}
		})
	}
}

// A character literal holding a quote ('"', '\”) must not open a string
// region: otherwise the comment right after it survives stripping (its
// words become dead-code references) and every line up to the next real
// string literal is swallowed into one STR token. Rust lifetimes ('a)
// share the quote character and must stay untouched.
func TestCharLiteralsDoNotOpenStrings(t *testing.T) {
	for _, tc := range []struct {
		lang        Language
		withComment string
		plain       string
	}{
		{Go,
			"func isQuote(c byte) bool {\n\treturn c == '\\'' || c == '\"' // ghostHelper\n}\n\nfunc greet() string {\n\treturn \"hi\"\n}\n",
			"func isQuote(c byte) bool {\n\treturn c == '\\'' || c == '\"'\n}\n\nfunc greet() string {\n\treturn \"hi\"\n}\n"},
		{Java,
			"boolean isQuote(char c) {\n\treturn c == '\\'' || c == '\"'; // ghostHelper\n}\n\nString greet() {\n\treturn \"hi\";\n}\n",
			"boolean isQuote(char c) {\n\treturn c == '\\'' || c == '\"';\n}\n\nString greet() {\n\treturn \"hi\";\n}\n"},
		{Rust,
			"fn is_quote(c: char) -> bool {\n\tc == '\\'' || c == '\"' // ghostHelper\n}\n\nfn first<'a>(s: &'a str) -> &'a str {\n\ts // ghostHelper\n}\n\nfn greet() -> String {\n\t\"hi\".to_string()\n}\n",
			"fn is_quote(c: char) -> bool {\n\tc == '\\'' || c == '\"'\n}\n\nfn first<'a>(s: &'a str) -> &'a str {\n\ts\n}\n\nfn greet() -> String {\n\t\"hi\".to_string()\n}\n"},
	} {
		t.Run(string(tc.lang), func(t *testing.T) {
			got, lines := TokenizeWithLines(tc.withComment, tc.lang)
			want, wantLines := TokenizeWithLines(tc.plain, tc.lang)
			if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(lines, wantLines) {
				t.Errorf("tokens/lines = %v / %v, want %v / %v", got, lines, want, wantLines)
			}
			if got, want := Normalize(tc.withComment, tc.lang), Normalize(tc.plain, tc.lang); got != want {
				t.Errorf("normalized = %q, want %q", got, want)
			}
			refs := refWords(References(tc.withComment, tc.lang))
			if len(refs["ghostHelper"]) != 0 {
				t.Errorf("comment after a char literal produced references: %v", refs["ghostHelper"])
			}
			if len(refs["greet"]) == 0 {
				t.Error("definition after a char literal lost its reference")
			}
		})
	}
}
