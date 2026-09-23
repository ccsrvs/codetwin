# Block-clone NEGATIVE fixture (negative/frame-boilerplate-asm-gas).
# The only commonality is the System V frame prologue/epilogue and a
# callee-saved register spill: well under 8 shared lines.
	.text
	.globl	mix_hash
	.type	mix_hash, @function
mix_hash:
	pushq	%rbp
	movq	%rsp, %rbp
	pushq	%rbx
	movq	%rdi, %rax
	movabsq	$0x9e3779b97f4a7c15, %rbx
	imulq	%rbx, %rax
	movq	%rax, %rdx
	shrq	$29, %rdx
	xorq	%rdx, %rax
	imulq	%rbx, %rax
	rolq	$17, %rax
	popq	%rbx
	popq	%rbp
	ret
	.size	mix_hash, .-mix_hash
