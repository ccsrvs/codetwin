#include "asm.S"

// void add_rows_u8(uint8_t *dst, const uint8_t *src, size_t n)
function add_rows_u8, export=1
        cbz             x2, 9f
1:
        ld1             {v0.16b}, [x0]
        ld1             {v1.16b}, [x1], #16
        uqadd           v0.16b, v0.16b, v1.16b
        st1             {v0.16b}, [x0], #16
        subs            x2, x2, #16
        b.gt            1b
9:
        ret
endfunc
