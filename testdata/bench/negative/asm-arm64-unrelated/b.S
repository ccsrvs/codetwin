#include "asmdefs.h"

// void set_words(uint32_t *dst, uint32_t value, size_t count)
ENTRY (set_words)
	dup	v0.4s, w1
	lsr	x3, x2, #2
	cbz	x3, 2f
1:	st1	{v0.4s}, [x0], #16
	subs	x3, x3, #1
	b.ne	1b
2:	and	x2, x2, #3
	cbz	x2, 4f
3:	str	w1, [x0], #4
	subs	x2, x2, #1
	b.ne	3b
4:	ret
END (set_words)
