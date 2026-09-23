#include "asmdefs.h"

// size_t str_len(const char *s)
ENTRY (str_len)
	mov	x1, x0
1:	ldrb	w2, [x1], #1
	cbnz	w2, 1b
	sub	x0, x1, x0
	sub	x0, x0, #1
	ret
END (str_len)
