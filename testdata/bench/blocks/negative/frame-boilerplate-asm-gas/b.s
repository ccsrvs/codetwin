# Block-clone NEGATIVE fixture (negative/frame-boilerplate-asm-gas), b side.
	.text
	.globl	clamp_add
	.type	clamp_add, @function
clamp_add:
	pushq	%rbp
	movq	%rsp, %rbp
	pushq	%rbx
	movl	%edi, %eax
	addl	%esi, %eax
	jno	.Lok
	movl	$0x7fffffff, %eax
	testl	%esi, %esi
	jg	.Lok
	movl	$0x80000000, %eax
.Lok:
	popq	%rbx
	popq	%rbp
	ret
	.size	clamp_add, .-clamp_add
