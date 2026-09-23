# Block-clone fixture (positive/verbatim-asm-gas), review §5.3.
# Shared verbatim block: a.s lines 11-22 == b.s lines 13-24 (argument
# validation). The hosts differ: a.s sums 32-bit words, b.s copies
# bytes backwards.
	.text
	.globl	sum_words
	.type	sum_words, @function
sum_words:
	pushq	%rbx
	xorl	%eax, %eax
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
.Lsum:
	addl	(%rdx), %eax
	adcl	$0, %eax
	addq	$4, %rdx
	decq	%rcx
	jne	.Lsum
	movl	%eax, %edx
	shrl	$16, %edx
	addw	%dx, %ax
	adcw	$0, %ax
	notw	%ax
	popq	%rbx
	ret
.Lbad:
	movl	$-1, %eax
	popq	%rbx
	ret
	.size	sum_words, .-sum_words
