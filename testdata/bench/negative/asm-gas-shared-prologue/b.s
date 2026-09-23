# unsigned popcount64(uint64_t x): SWAR bit count.
	.text
	.globl	popcount64
	.type	popcount64, @function
popcount64:
	pushq	%rbp
	movq	%rsp, %rbp
	movq	%rdi, %rax
	shrq	$1, %rax
	movabsq	$0x5555555555555555, %rdx
	andq	%rdx, %rax
	subq	%rax, %rdi
	movabsq	$0x3333333333333333, %rdx
	movq	%rdi, %rax
	andq	%rdx, %rax
	shrq	$2, %rdi
	andq	%rdx, %rdi
	addq	%rdi, %rax
	movq	%rax, %rdx
	shrq	$4, %rdx
	addq	%rdx, %rax
	movabsq	$0x0f0f0f0f0f0f0f0f, %rdx
	andq	%rdx, %rax
	movabsq	$0x0101010101010101, %rdx
	imulq	%rdx, %rax
	shrq	$56, %rax
	popq	%rbp
	ret
	.size	popcount64, .-popcount64
