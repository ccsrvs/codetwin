package tokenizer

import "testing"

func refWords(refs []Ref) map[string][]int {
	out := map[string][]int{}
	for _, r := range refs {
		out[r.Word] = append(out[r.Word], r.Line)
	}
	return out
}

func TestReferencesStripsCommentsKeepsStrings(t *testing.T) {
	code := "// mentions oldHelper in a comment\n" +
		"func caller() {\n" +
		"\tuse(\"dynamicName\")\n" +
		"\trealHelper()\n" +
		"}\n"
	got := refWords(References(code, Go))

	if _, ok := got["oldHelper"]; ok {
		t.Errorf("comment-only mention oldHelper should be stripped, got lines %v", got["oldHelper"])
	}
	if lines, ok := got["dynamicName"]; !ok || lines[0] != 3 {
		t.Errorf("string-literal mention dynamicName should survive on line 3, got %v", lines)
	}
	if lines, ok := got["realHelper"]; !ok || lines[0] != 4 {
		t.Errorf("realHelper should be found on line 4, got %v", lines)
	}
}

func TestReferencesLineNumbersSurviveBlockComments(t *testing.T) {
	code := "/* block\ncomment\nspanning lines */\nafter()\n"
	got := refWords(References(code, Go))
	if lines, ok := got["after"]; !ok || lines[0] != 4 {
		t.Errorf("after() should be attributed to line 4 despite the block comment, got %v", lines)
	}
	if _, ok := got["comment"]; ok {
		t.Errorf("words inside a block comment should be stripped")
	}
}

func TestReferencesPythonHashComments(t *testing.T) {
	code := "# deadFn mentioned here only\nlive_fn()\n"
	got := refWords(References(code, Python))
	if _, ok := got["deadFn"]; ok {
		t.Errorf("hash-comment mention should be stripped")
	}
	if _, ok := got["live_fn"]; !ok {
		t.Errorf("live_fn call should be found")
	}
}

func TestReferencesUnknownLangFallsBack(t *testing.T) {
	got := refWords(References("something()", Unknown))
	if _, ok := got["something"]; !ok {
		t.Errorf("Unknown language should still extract identifiers")
	}
}
