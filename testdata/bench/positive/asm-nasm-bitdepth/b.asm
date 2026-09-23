%include "config.asm"
%include "ext/x86/x86inc.asm"

SECTION_RODATA
pw_1023: times 8 dw 1023

SECTION .text

INIT_XMM sse4
cglobal blend_16bpc, 4, 4, 6, dst, ds, tmp, w
    pxor            m4, m4
    mova            m5, [pw_1023]
.loop:
    movq            m0, [dstq]
    movq            m1, [tmpq]
    punpcklbw       m0, m4
    punpcklbw       m1, m4
    psubw           m1, m0
    pmulhrsw        m1, m5
    paddw           m0, m1
    packusdw        m0, m0
    movq        [dstq], m0
    add           dstq, dsq
    add           tmpq, 16
    dec             wd
    jg .loop
    RET
