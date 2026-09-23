# Block-clone fixture (positive/verbatim-asm-gas), b side.
# Shared verbatim block: b.s lines 13-24 == a.s lines 11-22.
	.text
	.globl	copy_backward
	.type	copy_backward, @function
copy_backward:
	pushq	%rbx
	pushq	%r12
	movq	%rdx, %r12
	xorl	%eax, %eax
	testq	%rdi, %rdi
	je	.Lfail
	testq	%rdi, %rdi
	je	.Lbad
	testq	%rsi, %rsi
	je	.Lbad
	cmpq	$4096, %rsi
	ja	.Lbad
	movq	%rdi, %rbx
	andq	$3, %rbx
	jne	.Lbad
	movq	%rsi, %rcx
	shrq	$2, %rcx
	movq	%rdi, %rdx
	std
	leaq	-1(%r12,%rsi), %rdi
	leaq	-1(%rdx,%rsi), %rsi
	movq	%r12, %rcx
	rep movsb
	cld
	movq	%r12, %rax
	popq	%r12
	popq	%rbx
	ret
.Lbad:
.Lfail:
	movq	$-22, %rax
	popq	%r12
	popq	%rbx
	ret
	.size	copy_backward, .-copy_backward
