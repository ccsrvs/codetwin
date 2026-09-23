#include "asm.S"

// void add_rows_u16(uint16_t *dst, const uint16_t *src, size_t n)
function add_rows_u16, export=1
        cbz             x2, 9f
1:
        ld1             {v0.8h}, [x0]
        ld1             {v1.8h}, [x1], #16
        uqadd           v0.8h, v0.8h, v1.8h
        st1             {v0.8h}, [x0], #16
        subs            x2, x2, #8
        b.gt            1b
9:
        ret
endfunc
