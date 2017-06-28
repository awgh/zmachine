package zmachine

import "fmt"

const (
	OPERAND_LARGE    = 0x0
	OPERAND_SMALL    = 0x1
	OPERAND_VARIABLE = 0x2
	OPERAND_OMITTED  = 0x3

	FORM_SHORT    = 0x0
	FORM_LONG     = 0x1
	FORM_VARIABLE = 0x2

	MAX_STACK  = 1024
	MAX_OBJECT = 256

	OBJECT_ENTRY_SIZE    = 9
	OBJECT_PARENT_INDEX  = 4
	OBJECT_SIBLING_INDEX = 5
	OBJECT_CHILD_INDEX   = 6
	NULL_OBJECT_INDEX    = 0

	DICT_NOT_FOUND = 0
)

var alphabets = []string{"abcdefghijklmnopqrstuvwxyz",
	"ABCDEFGHIJKLMNOPQRSTUVWXYZ",
	" \n0123456789.,!?_#'\"/\\-:()"}

type ZHeader struct {
	Version           uint8
	hiMemBase         uint16
	ip                uint16
	dictAddress       uint32
	objTableAddress   uint32
	globalVarAddress  uint32
	staticMemAddress  uint32
	abbreviationTable uint32
}

func (h *ZHeader) Read(buf []byte) {

	h.Version = buf[0]
	h.hiMemBase = GetUint16(buf, 4)
	h.ip = GetUint16(buf, 6)
	h.dictAddress = uint32(GetUint16(buf, 0x8))
	h.objTableAddress = uint32(GetUint16(buf, 0xA))
	h.globalVarAddress = uint32(GetUint16(buf, 0xC))
	h.staticMemAddress = uint32(GetUint16(buf, 0xE))
	h.abbreviationTable = uint32(GetUint16(buf, 0x18))

	DebugPrintf("End of dyn mem: 0x%X\n", h.staticMemAddress)
	DebugPrintf("Global vars: 0x%X\n", h.globalVarAddress)
}

var ZFunctions_VAR = []ZFunction{
	ZCall,
	ZStoreW,
	ZStoreB,
	ZPutProp,
	ZRead,
	ZPrintChar,
	ZPrintNum,
	ZRandom,
	ZPush,
	ZPull,
}

var ZFunctions_2OP = []ZFunction{
	ZNOP_VAR,
	ZJumpEqual,
	ZJumpLess,
	ZJumpGreater,
	ZDecChk,
	ZIncChk,
	ZJin,
	ZTest,
	ZOr,
	ZAnd,
	ZTestAttr,
	ZSetAttr,
	ZClearAttr,
	ZStore,
	ZInsertObj,
	ZLoadW,
	ZLoadB,
	ZGetProp,
	ZGetPropAddr,
	ZGetNextProp,
	ZAdd,
	ZSub,
	ZMul,
	ZDiv,
	ZMod,
}

var ZFunctions_1OP = []ZFunction1Op{
	ZJumpZero,
	ZGetSibling,
	ZGetChild,
	ZGetParent,
	ZGetPropLen,
	ZInc,
	ZDec,
	ZPrintAddr,
	ZNOP1,
	ZRemoveObj,
	ZPrintObj,
	ZRet,
	ZJump,
	ZPrintPAddr,
	ZLoad,
	ZNOP1,
}

var ZFunctions_0P = []ZFunction0Op{
	ZReturnTrue,
	ZReturnFalse,
	ZPrint,
	ZPrintRet,
	ZNOP0,
	ZNOP0,
	ZNOP0,
	ZNOP0,
	ZRetPopped,
	ZPop,
	ZQuit,
	ZNewLine,
}

type ZFunction func(*ZMachine, []uint16, uint16)
type ZFunction1Op func(*ZMachine, uint16)
type ZFunction0Op func(*ZMachine)

// " Given a packed address P, the formula to obtain the corresponding byte address B is:
//  2P           Versions 1, 2 and 3"
func PackedAddress(a uint32) uint32 {
	return a * 2
}

func DebugPrintf(format string, v ...interface{}) {
	//fmt.Printf(format, v...)
}

func GetUint16(buf []byte, offset uint32) uint16 {
	return (uint16(buf[offset]) << 8) | (uint16)(buf[offset+1])
}

func GetUint32(buf []byte, offset uint32) uint32 {
	return (uint32(buf[offset]) << 24) | (uint32(buf[offset+1]) << 16) | (uint32(buf[offset+2]) << 8) | uint32(buf[offset+3])
}

func PrintZChar(ch uint16) {
	if ch == 13 {
		fmt.Printf("\n")
	} else if ch >= 32 && ch <= 126 { // ASCII
		fmt.Printf("%c", ch)
	} // else ... do not bother
}
