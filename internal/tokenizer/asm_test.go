package tokenizer

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestDetect_AssemblyDialects(t *testing.T) {
	for _, tc := range []struct {
		name, file, code string
		want             Language
	}{
		{"plan9 TEXT", "memmove_amd64.s", "#include \"textflag.h\"\n\nTEXT ·Memmove(SB), NOSPLIT, $0-24\n\tMOVQ to+0(FP), DI\n\tRET\n", AsmPlan9},
		{"plan9 by textflag include alone", "stub_arm64.s", "#include \"textflag.h\"\n", AsmPlan9},
		{"nasm x86inc", "mc.asm", "%include \"config.asm\"\nSECTION .text\ncglobal put_8bpc, 3, 3, 8, dst, src\n    RET\n", AsmNASM},
		{"nasm plain", "start.asm", "global _start\nsection .text\n_start:\n    mov eax, 60\n    syscall\n", AsmNASM},
		{"masm proc", "cpuid.asm", ".code\nGetCpuid PROC\n    push rbx\n    ret\nGetCpuid ENDP\nEND\n", AsmMASM},
		{"armasm", "jump.asm", "    AREA |.text|, CODE, READONLY\n    EXPORT jump_fcontext\njump_fcontext PROC\n    ret\n    ENDP\n    END\n", AsmMASM},
		{"gas x86 with .data is not masm", "setjmp.s", ".data\n.global setjmp\n.type setjmp,@function\nsetjmp:\n\tmov %rbx,(%rdi)\n\tret\n", AsmGAS},
		{"gas arm64", "memcpy.S", "#include \"asmdefs.h\"\nENTRY (memcpy)\n\tldp x6, x7, [x1, #16]\n\tret\nEND (memcpy)\n", AsmGAS},
		{"uppercase .ASM extension", "HELLO.ASM", "MAIN PROC\n    ret\nMAIN ENDP\n", AsmMASM},
		{"no content: .asm defaults to nasm", "x.asm", "", AsmNASM},
		{"no content: .S defaults to gas", "x.S", "", AsmGAS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Detect(tc.file, tc.code); got != tc.want {
				t.Errorf("Detect(%q) = %v, want %v", tc.file, got, tc.want)
			}
		})
	}
	if !IsAssembly(AsmGAS) || !IsAssembly(AsmPlan9) || IsAssembly(C) {
		t.Error("IsAssembly misclassifies languages")
	}
}

// GAS uses one comment rule for every architecture: a # at line start
// or after whitespace-then-space, and ARM32's "@ " — while ARM "#imm"
// operands and x86 "@function"/"@PLT" survive. A surviving comment word
// would normalize to VAR, so the check compares against the same code
// with its comments removed by hand.
func TestTokenize_GASCommentsKeepImmediatesAndAtOperators(t *testing.T) {
	code := "# file comment\n" +
		"\t.type f, @function   # trailing comment\n" +
		"f:\n" +
		"\tmov x0, #16          // arm64 comment\n" +
		"\tldr r1, [r2, #4]     @ arm32 comment\n" +
		"\tcall g@PLT           /* block */\n" +
		"\t.asciz \"a # b @ c\"\n"
	clean := "\n" +
		"\t.type f, @function\n" +
		"f:\n" +
		"\tmov x0, #16\n" +
		"\tldr r1, [r2, #4]\n" +
		"\tcall g@PLT\n" +
		"\t.asciz \"a # b @ c\"\n"
	got, want := Tokenize(code, AsmGAS), Tokenize(clean, AsmGAS)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tokens with comments:\n%v\nwithout:\n%v", got, want)
	}
	joined := strings.Join(got, " ")
	for _, kept := range []string{"# NUM", "@ function", "@ plt", "STR"} {
		if !strings.Contains(joined, kept) {
			t.Errorf("%q missing from %s", kept, joined)
		}
	}
}

func TestTokenize_SemicolonDialectsAndPlan9Comments(t *testing.T) {
	for _, tc := range []struct {
		lang        Language
		code, clean string
	}{
		{AsmNASM, "msg db \"a;b\", 0 ; ghostWord\n    mov eax, 1 ; ghostWord\n", "msg db \"a;b\", 0\n    mov eax, 1\n"},
		{AsmMASM, "msg db 'a;b', 0 ; ghostWord\n    mov eax, 1 ; ghostWord\n", "msg db 'a;b', 0\n    mov eax, 1\n"},
		{AsmPlan9, "#include \"textflag.h\"\n// ghostWord\nTEXT ·f(SB), NOSPLIT, $0\n\tMOVQ $1, AX /* ghostWord */\n\tRET\n", "\n\nTEXT ·f(SB), NOSPLIT, $0\n\tMOVQ $1, AX\n\tRET\n"},
	} {
		got, want := Tokenize(tc.code, tc.lang), Tokenize(tc.clean, tc.lang)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: comment or include survived:\n%v\nwant\n%v", tc.lang, got, want)
		}
		if tc.lang != AsmPlan9 && !slices.Contains(got, "STR") {
			t.Errorf("%s: string with ';' inside was cut: %v", tc.lang, got)
		}
	}
}

// Mnemonics, directives and registers are the structure of assembly;
// labels and symbols are names. Renaming labels and symbols must not
// change the token stream, and changing an instruction must.
func TestTokenize_AssemblyNormalizesNamesKeepsInstructions(t *testing.T) {
	for _, tc := range []struct {
		lang            Language
		a, renamed, alt string
	}{
		{AsmGAS,
			"memset_a:\n\tmovq %rdi, %rax\n\ttestq %rdx, %rdx\n\tje .Ldone_a\n.Lloop_a:\n\tmovb %sil, (%rdi)\n\tincq %rdi\n\tdecq %rdx\n\tjnz .Lloop_a\n.Ldone_a:\n\tret\n",
			"fill_bytes:\n\tmovq %rdi, %rax\n\ttestq %rdx, %rdx\n\tje .Lexit\n.Lfill:\n\tmovb %sil, (%rdi)\n\tincq %rdi\n\tdecq %rdx\n\tjnz .Lfill\n.Lexit:\n\tret\n",
			"memset_a:\n\tmovq %rdi, %rax\n\ttestq %rdx, %rdx\n\tje .Ldone_a\n.Lloop_a:\n\tmovb %sil, (%rdi)\n\taddq $1, %rdi\n\tdecq %rdx\n\tjnz .Lloop_a\n.Ldone_a:\n\tret\n"},
		{AsmGAS, // numeric local labels: 1b and 2b are the same kind of reference
			"f:\n\tsubs x2, x2, #1\n\tb.ne 1b\n\tld1 {v0.16b}, [x0], #16\n",
			"g:\n\tsubs x2, x2, #1\n\tb.ne 2b\n\tld1 {v0.16b}, [x0], #16\n",
			"f:\n\tsubs x2, x2, #1\n\tb.eq 1b\n\tld1 {v0.16b}, [x0], #16\n"},
		{AsmPlan9,
			"TEXT ·Add(SB), NOSPLIT, $0-24\n\tMOVQ a+0(FP), AX\n\tADDQ b+8(FP), AX\n\tMOVQ AX, ret+16(FP)\n\tRET\n",
			"TEXT ·sum(SB), NOSPLIT, $0-24\n\tMOVQ x+0(FP), AX\n\tADDQ y+8(FP), AX\n\tMOVQ AX, r+16(FP)\n\tRET\n",
			"TEXT ·Add(SB), NOSPLIT, $0-24\n\tMOVQ a+0(FP), AX\n\tSUBQ b+8(FP), AX\n\tMOVQ AX, ret+16(FP)\n\tRET\n"},
		{AsmNASM,
			"cglobal copy_px, 3, 3, 2, dst, src, len\n.loop:\n    movu m0, [srcq]\n    movu [dstq], m0\n    add srcq, 16\n    dec lenq\n    jg .loop\n    RET\n",
			"cglobal move_row, 3, 3, 2, out, in, n\n.next:\n    movu m0, [inq]\n    movu [outq], m0\n    add inq, 16\n    dec nq\n    jg .next\n    RET\n",
			"cglobal copy_px, 3, 3, 2, dst, src, len\n.loop:\n    movu m0, [srcq]\n    movu [dstq], m1\n    add srcq, 16\n    dec lenq\n    jg .loop\n    RET\n"},
		{AsmMASM,
			"Checksum PROC\n    xor eax, eax\n@@:\n    add al, [rcx]\n    inc rcx\n    dec rdx\n    jnz @B\n    ret\nChecksum ENDP\n",
			"SumBytes PROC\n    xor eax, eax\n@@:\n    add al, [rcx]\n    inc rcx\n    dec rdx\n    jnz @B\n    ret\nSumBytes ENDP\n",
			"Checksum PROC\n    xor eax, eax\n@@:\n    sub al, [rcx]\n    inc rcx\n    dec rdx\n    jnz @B\n    ret\nChecksum ENDP\n"},
	} {
		t.Run(string(tc.lang), func(t *testing.T) {
			a, lines := TokenizeWithLines(tc.a, tc.lang)
			r, _ := TokenizeWithLines(tc.renamed, tc.lang)
			if !reflect.DeepEqual(a, r) {
				t.Errorf("renamed labels/symbols changed the tokens:\n%v\n%v", a, r)
			}
			if alt := Tokenize(tc.alt, tc.lang); reflect.DeepEqual(a, alt) {
				t.Errorf("a changed instruction or register did not change the tokens: %v", a)
			}
			if len(lines) != len(a) || lines[len(lines)-1] > strings.Count(tc.a, "\n") {
				t.Errorf("line numbers out of range: %v", lines)
			}
			if got, want := Normalize(tc.a, tc.lang), Normalize(tc.renamed, tc.lang); got != want {
				t.Errorf("Normalize differs for renamed code:\n%q\n%q", got, want)
			}
		})
	}
}

func TestTokenize_AssemblyNameFirstStatements(t *testing.T) {
	// NASM/MASM data and constant definitions put the name first and
	// the keyword second: the name normalizes, the keyword stays.
	for _, lang := range []Language{AsmNASM, AsmMASM} {
		a := Tokenize("msg db 'hello', 0\nlen equ 5\n", lang)
		b := Tokenize("greeting db 'hi', 0\ncount equ 9\n", lang)
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s: renamed data definitions differ:\n%v\n%v", lang, a, b)
		}
		if !slices.Contains(a, "db") || !slices.Contains(a, "equ") {
			t.Errorf("%s: data keywords normalized away: %v", lang, a)
		}
	}
}

func TestLexicalTermsAndReferences_Assembly(t *testing.T) {
	code := "\t.globl checksum_block\nchecksum_block:\n\tmovq buffer_len(%rip), %rcx\n\tcall update_crc  # not ghostHelper\n\tret\n"
	terms := LexicalTerms(code, AsmGAS)
	for _, want := range []string{"checksum", "block", "buffer", "len", "update", "crc"} {
		if !slices.Contains(terms, want) {
			t.Errorf("term %q missing from %v", want, terms)
		}
	}
	for _, unwanted := range []string{"movq", "rcx", "rip", "call", "ret", "globl", "ghost"} {
		if slices.Contains(terms, unwanted) {
			t.Errorf("instruction, register or comment term %q in %v", unwanted, terms)
		}
	}
	refs := refWords(References(code, AsmGAS))
	if !reflect.DeepEqual(refs["update_crc"], []int{4}) || len(refs["ghostHelper"]) != 0 {
		t.Errorf("references = %v", refs)
	}
}

// Keyword sets are per dialect: a Go argument named `length` and a
// label named `rest` are names in Plan 9, even though LENGTH is a MASM
// operator and `rest` is NASM's reserve-ten-bytes directive — and even
// in NASM, `jb rest` jumps to a label.
func TestTokenize_AssemblyKeywordsArePerDialect(t *testing.T) {
	for _, tc := range []struct {
		lang   Language
		a, b   string
		reason string
	}{
		{AsmPlan9, "\tMOVQ n+24(FP), DX\n", "\tMOVQ length+24(FP), DX\n", "Go argument named length"},
		{AsmPlan9, "\tJB tail\n", "\tJB rest\n", "Plan 9 label named rest"},
		{AsmNASM, "    jb tail\n", "    jb rest\n", "NASM label named rest"},
	} {
		if a, b := Tokenize(tc.a, tc.lang), Tokenize(tc.b, tc.lang); !reflect.DeepEqual(a, b) {
			t.Errorf("%s: %v vs %v", tc.reason, a, b)
		}
	}
	// NASM data definitions still put the name first.
	if got := strings.Join(Tokenize("buf rest 4\n", AsmNASM), " "); got != "VAR rest NUM" {
		t.Errorf("NASM `buf rest 4` = %q", got)
	}
}

// AArch64 arrangement specifiers (.16b, .8h, .4s) normalize like ARM32's
// word-shaped element types (vadd.i16), so element-size variants of the
// same routine match.
func TestTokenize_AArch64ArrangementsNormalize(t *testing.T) {
	a := Tokenize("\tld1 {v0.16b}, [x1], #16\n\tuqadd v0.16b, v0.16b, v1.16b\n", AsmGAS)
	b := Tokenize("\tld1 {v0.8h}, [x1], #16\n\tuqadd v0.8h, v0.8h, v1.8h\n", AsmGAS)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("arrangements did not normalize:\n%v\n%v", a, b)
	}
}
