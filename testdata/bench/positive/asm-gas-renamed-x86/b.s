# Count how many bytes of buf equal the given value.
	.text
	.globl	tally_byte
	.type	tally_byte, @function
tally_byte:
	xorl	%eax, %eax
	testq	%rsi, %rsi
	je	.Lend
.Lscan:
	movzbl	(%rdi), %ecx
	cmpl	%edx, %ecx
	jne	.Lskip
	incq	%rax
.Lskip:
	incq	%rdi
	decq	%rsi
	jne	.Lscan
.Lend:
	ret
	.size	tally_byte, .-tally_byte
