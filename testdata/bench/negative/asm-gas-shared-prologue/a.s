# void reverse_bytes(char *buf, size_t n): reverse in place.
	.text
	.globl	reverse_bytes
	.type	reverse_bytes, @function
reverse_bytes:
	pushq	%rbp
	movq	%rsp, %rbp
	leaq	-1(%rdi,%rsi), %rsi
.Lswap:
	cmpq	%rsi, %rdi
	jae	.Lout
	movb	(%rdi), %al
	movb	(%rsi), %cl
	movb	%cl, (%rdi)
	movb	%al, (%rsi)
	incq	%rdi
	decq	%rsi
	jmp	.Lswap
.Lout:
	popq	%rbp
	ret
	.size	reverse_bytes, .-reverse_bytes
