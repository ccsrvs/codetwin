#include "textflag.h"

// func xorInto(out, x, y *byte, length int)
TEXT ·xorInto(SB), NOSPLIT, $0-32
	MOVQ out+0(FP), BX
	MOVQ x+8(FP), SI
	MOVQ y+16(FP), CX
	MOVQ length+24(FP), DX
	XORQ AX, AX
	CMPQ DX, $16
	JB   rest

blocks:
	MOVOU (SI)(AX*1), X0
	MOVOU (CX)(AX*1), X1
	PXOR  X1, X0
	MOVOU X0, (BX)(AX*1)
	ADDQ  $16, AX
	SUBQ  $16, DX
	CMPQ  DX, $16
	JAE   blocks

rest:
	TESTQ DX, DX
	JZ    finish
	MOVB  (SI)(AX*1), R8
	XORB  (CX)(AX*1), R8
	MOVB  R8, (BX)(AX*1)
	INCQ  AX
	DECQ  DX
	JMP   rest

finish:
	RET
