package deadcode

import "testing"

func TestAssemblyDeadCode(t *testing.T) {
	snippets := scanDir(t, map[string]string{
		// Declaration directives (.globl/.type/.size) are not uses; a
		// call is. Exported assembly is advisory: callers may be outside
		// the scan.
		"x86/routines.S": "\t.text\n" +
			"\t.globl sum_bytes\n\t.type sum_bytes, @function\n" +
			"sum_bytes:\n\txorl %eax, %eax\n\taddb (%rdi), %al\n\tret\n\t.size sum_bytes, .-sum_bytes\n\n" +
			"\t.globl lonely\n\t.type lonely, @function\n" +
			"lonely:\n\tcall sum_bytes\n\tmovq %rax, %rdx\n\tret\n\t.size lonely, .-lonely\n\n" +
			"\t.globl called_from_c\n" +
			"called_from_c:\n\tmovq %rdi, %rax\n\tshlq $1, %rax\n\tret\n\n" +
			// Mach-O: C's fast_copy is _fast_copy in assembly, and a call
			// to _c_helper reaches C's c_helper.
			"\t.globl _fast_copy\n" +
			"_fast_copy:\n\tmovq %rsi, %rcx\n\trep movsb\n\tcall _c_helper\n\tret\n",
		"main.c": "extern long called_from_c(long);\nextern void fast_copy(void *, const void *);\n" +
			"int c_helper(int x)\n{\n\treturn x * 3;\n}\n" +
			"int main(void)\n{\n\tchar a[4], b[4] = {0};\n\tfast_copy(a, b);\n\treturn (int)called_from_c(2);\n}\n",
		// x86inc names are token-pasted (x##_8bpc_##cpu) and never
		// appear literally in C; dav1d's `function` macro likewise.
		"x86/mc.asm": "%include \"x86inc.asm\"\nSECTION .text\nINIT_XMM ssse3\ncglobal put_bilin_8bpc, 4, 4, 2, dst, src\n    movu m0, [srcq]\n    movu [dstq], m0\n    RET\n",
		"arm/mc.S":   "#include \"asm.S\"\nfunction avg_8bpc_neon, export=1\n\tld1 {v0.16b}, [x2], #16\n\tst1 {v0.16b}, [x0], #16\n\tret\nendfunc\n",
	})
	findings, warnings := Analyze(snippets)
	if len(warnings) > 0 {
		t.Fatalf("warnings: %v", warnings)
	}
	got := findingsBySymbol(findings)
	if f, ok := got["lonely"]; !ok || f.Verdict != VerdictUnusedInScan {
		t.Errorf("lonely (only its own directives mention it): want unused-in-scan, got %+v (present=%v)", f, ok)
	}
	for _, alive := range []string{"sum_bytes", "called_from_c", "_fast_copy", "c_helper", "main", "put_bilin_8bpc", "avg_8bpc_neon"} {
		if f, ok := got[alive]; ok {
			t.Errorf("%s must not be reported, got %+v", alive, f)
		}
	}
	for _, f := range findings {
		if f.Verdict == VerdictDead && f.Lang != "c" {
			t.Errorf("assembly findings must stay advisory, got %+v", f)
		}
	}
}
