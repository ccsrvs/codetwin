%include "config.asm"
%include "ext/x86/x86inc.asm"

SECTION_RODATA
pw_512: times 8 dw 512

SECTION .text

INIT_XMM ssse3
cglobal blend_8bpc, 4, 4, 6, dst, ds, tmp, w
    pxor            m4, m4
    mova            m5, [pw_512]
.loop:
    movq            m0, [dstq]
    movq            m1, [tmpq]
    punpcklbw       m0, m4
    punpcklbw       m1, m4
    psubw           m1, m0
    pmulhrsw        m1, m5
    paddw           m0, m1
    packuswb        m0, m0
    movq        [dstq], m0
    add           dstq, dsq
    add           tmpq, 8
    dec             wd
    jg .loop
    RET
