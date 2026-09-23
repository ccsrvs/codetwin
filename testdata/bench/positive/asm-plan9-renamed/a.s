#include "textflag.h"

// func xorBytes(dst, a, b *byte, n int)
TEXT ·xorBytes(SB), NOSPLIT, $0-32
	MOVQ dst+0(FP), BX
	MOVQ a+8(FP), SI
	MOVQ b+16(FP), CX
	MOVQ n+24(FP), DX
	XORQ AX, AX
	CMPQ DX, $16
	JB   tail

loop16:
	MOVOU (SI)(AX*1), X0
	MOVOU (CX)(AX*1), X1
	PXOR  X1, X0
	MOVOU X0, (BX)(AX*1)
	ADDQ  $16, AX
	SUBQ  $16, DX
	CMPQ  DX, $16
	JAE   loop16

tail:
	TESTQ DX, DX
	JZ    done
	MOVB  (SI)(AX*1), R8
	XORB  (CX)(AX*1), R8
	MOVB  R8, (BX)(AX*1)
	INCQ  AX
	DECQ  DX
	JMP   tail

done:
	RET
