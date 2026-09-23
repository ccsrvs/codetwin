# size_t count_char(const char *s, size_t n, int c)
	.text
	.globl	count_char
	.type	count_char, @function
count_char:
	xorl	%eax, %eax
	testq	%rsi, %rsi
	je	.Ldone
.Lloop:
	movzbl	(%rdi), %ecx
	cmpl	%edx, %ecx
	jne	.Lnext
	incq	%rax
.Lnext:
	incq	%rdi
	decq	%rsi
	jne	.Lloop
.Ldone:
	ret
	.size	count_char, .-count_char
