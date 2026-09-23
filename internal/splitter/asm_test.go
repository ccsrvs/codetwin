package splitter

import (
	"reflect"
	"testing"

	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

func TestSplitAsm(t *testing.T) {
	for _, tc := range []struct {
		name string
		lang tokenizer.Language
		code string
		want []span
	}{
		{
			name: "GAS x86: .globl/.type labels end at .size; local, numeric and data labels stay inside or out",
			lang: tokenizer.AsmGAS,
			code: "\t.text\n" + // 1
				"\t.globl\tsum_bytes\n" + // 2
				"\t.type\tsum_bytes, @function\n" + // 3
				"sum_bytes:\n" + // 4
				"\txorl %eax, %eax\n" + // 5
				".Lloop:\n" + // 6
				"\taddb (%rdi), %al\n" + // 7
				"1:\tincq %rdi\n" + // 8
				"\tdecq %rsi\n" + // 9
				"\tjnz .Lloop\n" + // 10
				"\tret\n" + // 11
				"\t.size\tsum_bytes, .-sum_bytes\n" + // 12
				"\n" + // 13
				"\t.section .rodata\n" + // 14
				"table:\n" + // 15
				"\t.quad 1, 2, 3\n" + // 16
				"\n" + // 17
				"\t.text\n" + // 18
				"\t.globl xor_bytes\n" + // 19
				"\t.type xor_bytes,%function\n" + // 20
				"xor_bytes:\n" + // 21
				"\txorl %eax, %eax\n" + // 22
				"\tret\n" + // 23
				"\t.size xor_bytes, .-xor_bytes\n", // 24
			want: []span{{"sum_bytes", 4, 12}, {"xor_bytes", 21, 24}},
		},
		{
			name: "GAS: consecutive exported labels are one routine with aliases",
			lang: tokenizer.AsmGAS,
			code: ".global __setjmp\n.global _setjmp\n.global setjmp\n.type __setjmp,@function\n.type _setjmp,@function\n.type setjmp,@function\n" +
				"__setjmp:\n_setjmp:\nsetjmp:\n\tmov %rbx,(%rdi)\n\txor %eax,%eax\n\tret\n",
			want: []span{{"__setjmp", 7, 12}},
		},
		{
			name: "GAS: labels without .size end before the next routine, dropping its declarations",
			lang: tokenizer.AsmGAS,
			code: ".globl first\nfirst:\n\tmov x0, #1\n\tret\n\n\t.p2align 4\n\t.globl second\nsecond:\n\tmov x0, #2\n\tret\n",
			want: []span{{"first", 2, 4}, {"second", 8, 10}},
		},
		{
			name: "GAS: a .globl far from its label still marks it",
			lang: tokenizer.AsmGAS,
			code: "\t.globl far_away\n\t.text\n\t.p2align 4\n\n# helpers follow\n\nfar_away:\n\tadd x0, x0, x1\n\tret\n",
			want: []span{{"far_away", 7, 9}},
		},
		{
			name: "GAS: ENTRY/END, SYM_FUNC_START/END, and function/endfunc macros",
			lang: tokenizer.AsmGAS,
			code: "#include \"asmdefs.h\"\n" + // 1
				"ENTRY (memcpy_fast)\n" + // 2
				"\tldp x6, x7, [x1, #16]\n" + // 3
				"\tret\n" + // 4
				"END (memcpy_fast)\n" + // 5
				"\n" + // 6
				"SYM_FUNC_START(clear_page)\n" + // 7
				"\tmov x1, #0\n" + // 8
				"\tret\n" + // 9
				"SYM_FUNC_END(clear_page)\n" + // 10
				"\n" + // 11
				"function blend_w4_8bpc_neon, export=1\n" + // 12
				"1:\n" + // 13
				"\tld1 {v0.8b}, [x2], #8\n" + // 14
				"\tb.gt 1b\n" + // 15
				"\tret\n" + // 16
				"endfunc\n", // 17
			want: []span{{"memcpy_fast", 2, 5}, {"clear_page", 7, 10}, {"blend_w4_8bpc_neon", 12, 17}},
		},
		{
			name: "GAS: exported data objects are not routines, and a .macro ends the routine before it",
			lang: tokenizer.AsmGAS,
			code: "\t.globl pow_table\n" + // 1
				"\t.type pow_table, @object\n" + // 2
				"pow_table:\n" + // 3
				"\t.quad 1, 2, 4, 8\n" + // 4
				"\t.globl pow2\n" + // 5
				"pow2:\n" + // 6
				"\tmov (%rdi), %rax\n" + // 7
				"\tret\n" + // 8
				"\n" + // 9
				".macro load_pair a, b\n" + // 10
				"\tmov \\a, \\b\n" + // 11
				".endm\n", // 12
			want: []span{{"pow2", 6, 8}},
		},
		{
			name: "GAS: macro-wrapped names (EXT(x)) and COFF .def declarations",
			lang: tokenizer.AsmGAS,
			code: "#define EXT(s) _##s\n" + // 1
				".globl EXT(crosscall1)\n" + // 2
				"EXT(crosscall1):\n" + // 3
				"\tpushq %rbx\n" + // 4
				"\tret\n" + // 5
				"\n" + // 6
				".def\tjump_fcontext;\t.scl\t2;\t.type\t32;\t.endef\n" + // 7
				"jump_fcontext:\n" + // 8
				"\tret\n", // 9
			want: []span{{"crosscall1", 3, 5}, {"jump_fcontext", 8, 9}},
		},
		{
			name: "GAS: AIX XCOFF storage-class suffixes and dot-prefixed entry points",
			lang: tokenizer.AsmGAS,
			code: "    .globl  jump_fcontext[DS]\n" + // 1
				"    .globl .jump_fcontext\n" + // 2
				"    .csect  jump_fcontext[DS]\n" + // 3
				"jump_fcontext:\n" + // 4
				"    .long .jump_fcontext\n" + // 5
				"    .csect .text[PR], 5\n" + // 6
				".jump_fcontext:\n" + // 7
				"    mflr 0\n" + // 8
				"    blr\n", // 9
			want: []span{{"jump_fcontext", 4, 9}},
		},
		{
			name: "NASM: libjpeg-turbo GLOBAL_DATA tables are not routines",
			lang: tokenizer.AsmNASM,
			code: "    GLOBAL_DATA(jconst_fdct)\n" + // 1
				"EXTN(jconst_fdct):\n" + // 2
				"    times 8 dw 1\n" + // 3
				"    GLOBAL_FUNCTION(jsimd_fdct)\n" + // 4
				"EXTN(jsimd_fdct):\n" + // 5
				"    push ebp\n" + // 6
				"    ret\n", // 7
			want: []span{{"jsimd_fdct", 5, 7}},
		},
		{
			name: "NASM: a %macro definition ends the routine before it",
			lang: tokenizer.AsmNASM,
			code: "global first\n" + // 1
				"first:\n" + // 2
				"    ret\n" + // 3
				"%macro SHUF 2\n" + // 4
				"    pshufb %1, %2\n" + // 5
				"%endmacro\n", // 6
			want: []span{{"first", 2, 3}},
		},
		{
			name: "GAS: no exported routine falls back to the whole file",
			lang: tokenizer.AsmGAS,
			code: "helper:\n\tret\n",
			want: []span{{"", 1, 3}},
		},
		{
			name: "NASM: cglobal routines with local labels",
			lang: tokenizer.AsmNASM,
			code: "%include \"x86inc.asm\"\n" + // 1
				"SECTION .text\n" + // 2
				"INIT_XMM sse2\n" + // 3
				"cglobal copy_px, 3, 3, 2, dst, src, len\n" + // 4
				".loop:\n" + // 5
				"    movu m0, [srcq]\n" + // 6
				"    jg .loop\n" + // 7
				"    RET\n" + // 8
				"\n" + // 9
				"cglobal fill_px, 2, 2, 1, dst, len\n" + // 10
				"    pxor m0, m0\n" + // 11
				"    RET\n", // 12
			want: []span{{"copy_px", 4, 8}, {"fill_px", 10, 12}},
		},
		{
			name: "NASM: global + label, and cglobal inside a %macro ends at %endmacro",
			lang: tokenizer.AsmNASM,
			code: "global _start\n" + // 1
				"section .text\n" + // 2
				"_start:\n" + // 3
				"    mov eax, 60\n" + // 4
				"    syscall\n" + // 5
				"\n" + // 6
				"%macro AVG 1\n" + // 7
				"cglobal avg_%1, 4, 4\n" + // 8
				"    pavgb m0, m1\n" + // 9
				"    RET\n" + // 10
				"%endmacro\n" + // 11
				"AVG sse2\n", // 12
			want: []span{{"_start", 3, 5}, {"avg_%1", 8, 11}},
		},
		{
			name: "MASM PROC/ENDP and armasm bars",
			lang: tokenizer.AsmMASM,
			code: ".code\n" + // 1
				"GetCpuid PROC\n" + // 2
				"    push rbx\n" + // 3
				"    ret\n" + // 4
				"GetCpuid ENDP\n" + // 5
				"\n" + // 6
				"|jump_fcontext| PROC\n" + // 7
				"    ret\n" + // 8
				"    ENDP\n" + // 9
				"END\n", // 10
			want: []span{{"GetCpuid", 2, 5}, {"jump_fcontext", 7, 9}},
		},
		{
			name: "Plan 9 TEXT blocks end before the next TEXT, DATA or GLOBL",
			lang: tokenizer.AsmPlan9,
			code: "#include \"textflag.h\"\n" + // 1
				"\n" + // 2
				"// func Add(a, b int) int\n" + // 3
				"TEXT ·Add(SB), NOSPLIT, $0-24\n" + // 4
				"\tMOVQ a+0(FP), AX\n" + // 5
				"\tADDQ b+8(FP), AX\n" + // 6
				"\tMOVQ AX, ret+16(FP)\n" + // 7
				"\tRET\n" + // 8
				"\n" + // 9
				"TEXT runtime·memmove<ABIInternal>(SB), NOSPLIT, $0-24\n" + // 10
				"loop:\n" + // 11
				"\tJMP loop\n" + // 12
				"\n" + // 13
				"TEXT helper<>(SB), NOSPLIT, $0\n" + // 14
				"\tRET\n" + // 15
				"\n" + // 16
				"DATA mask<>+0(SB)/8, $0xff\n" + // 17
				"GLOBL mask<>(SB), RODATA, $8\n", // 18
			want: []span{{"Add", 4, 8}, {"memmove", 10, 12}, {"helper", 14, 15}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := spansOf(Split("x", tc.code, tc.lang))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Split =\n  %v\nwant\n  %v", got, tc.want)
			}
		})
	}
}
