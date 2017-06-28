package zmachine

import (
	"bufio"
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"
)

func ZCall(zm *ZMachine, args []uint16, numArgs uint16) {
	if numArgs == 0 {
		panic("Data corruption, call instruction requires at least 1 argument")
	}

	// Save return address
	zm.stack.Push(uint16(zm.ip>>16) & 0xFFFF)
	zm.stack.Push(uint16(zm.ip & 0xFFFF))

	functionAddress := PackedAddress(uint32(args[0]))
	DebugPrintf("Jumping to 0x%X [0x%X]\n", functionAddress, args[0])

	zm.ip = functionAddress

	// Save local frame (think EBP)
	zm.stack.SaveFrame()

	if zm.ip == 0 {
		ZReturnFalse(zm)

		return
	}

	// Local function variables on the stack
	numLocals := zm.ReadByte()

	// "When a routine is called, its local variables are created with initial values taken from the routine header.
	// Next, the arguments are written into the local variables (argument 1 into local 1 and so on)."
	numArgs-- // first argument is function address
	for i := 0; i < int(numLocals); i++ {
		localVar := zm.ReadUint16()

		if numArgs > 0 {
			localVar = args[i+1]
			numArgs--
		}
		zm.stack.Push(localVar)
	}
}

//  storew array word-index value
func ZStoreW(zm *ZMachine, args []uint16, numArgs uint16) {

	address := uint32(args[0] + args[1]*2)
	if !zm.IsSafeToWrite(address) {
		panic("Access violation")
	}

	zm.SetUint16(address, args[2])
}

func ZStoreB(zm *ZMachine, args []uint16, numArgs uint16) {

	address := uint32(args[0] + args[1])
	if !zm.IsSafeToWrite(address) {
		panic("Access violation")
	}

	zm.buf[address] = uint8(args[2])
}

func ZPutProp(zm *ZMachine, args []uint16, numArgs uint16) {
	zm.SetObjectProperty(args[0], args[1], args[2])
}

func ZRead(zm *ZMachine, args []uint16, numArgs uint16) {

	textAddress := args[0]
	maxChars := uint16(zm.buf[textAddress])
	if maxChars == 0 {
		panic("Invalid max chars")
	}
	maxChars--

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')

	input = strings.ToLower(input)
	input = strings.Trim(input, "\r\n")

	copy(zm.buf[textAddress+1:textAddress+maxChars], input)
	zm.buf[textAddress+uint16(len(input))+1] = 0

	var words []string
	var wordStarts []uint16
	var stringBuffer bytes.Buffer
	prevWordStart := uint16(1)
	for i := uint16(1); zm.buf[textAddress+i] != 0; i++ {
		ch := zm.buf[textAddress+i]
		if ch == ' ' {
			if prevWordStart < 0xFFFF {
				words = append(words, stringBuffer.String())
				wordStarts = append(wordStarts, prevWordStart)
				stringBuffer.Truncate(0)
			}
			prevWordStart = 0xFFFF
		} else {
			stringBuffer.WriteByte(ch)
			if prevWordStart == 0xFFFF {
				prevWordStart = i
			}
		}
	}
	// Last word
	if prevWordStart < 0xFFFF {
		words = append(words, stringBuffer.String())
		wordStarts = append(wordStarts, prevWordStart)
	}

	// TODO: include other separators, not only spaces

	parseAddress := uint32(args[1])
	maxTokens := zm.buf[parseAddress]
	//DebugPrintf("Max tokens: %d\n", maxTokens)
	parseAddress++
	numTokens := uint8(len(words))
	if numTokens > maxTokens {
		numTokens = maxTokens
	}
	zm.buf[parseAddress] = numTokens
	parseAddress++

	// "Each block consists of the byte address of the word in the dictionary, if it is in the dictionary, or 0 if it isn't;
	// followed by a byte giving the number of letters in the word; and finally a byte giving the position in the text-buffer
	// of the first letter of the word.
	for i, w := range words {

		if uint8(i) >= maxTokens {
			break
		}

		DebugPrintf("w = %s, %d\n", w, wordStarts[i])
		dictionaryAddress := zm.FindInDictionary(w)
		DebugPrintf("Dictionary address: 0x%X\n", dictionaryAddress)

		zm.SetUint16(parseAddress, dictionaryAddress)
		zm.buf[parseAddress+2] = uint8(len(w))
		zm.buf[parseAddress+3] = uint8(wordStarts[i])
		parseAddress += 4
	}
}

func ZPrintChar(zm *ZMachine, args []uint16, numArgs uint16) {
	ch := args[0]
	PrintZChar(ch)
}

func ZPrintNum(zm *ZMachine, args []uint16, numArgs uint16) {
	fmt.Printf("%d", int16(args[0]))
}

// If range is positive, returns a uniformly random number between 1 and range.
// If range is negative, the random number generator is seeded to that value and the return value is 0.
// Most interpreters consider giving 0 as range illegal (because they attempt a division with remainder by the range),
/// but correct behaviour is to reseed the generator in as random a way as the interpreter can (e.g. by using the time
// in milliseconds).
func ZRandom(zm *ZMachine, args []uint16, numArgs uint16) {
	randRange := int16(args[0])

	if randRange > 0 {
		r := rand.Int31n(int32(randRange)) // [0, n]
		zm.StoreResult(uint16(r + 1))
	} else if randRange < 0 {
		rand.Seed(int64(randRange * -1))
		zm.StoreResult(0)
	} else {
		rand.Seed(time.Now().Unix())
		zm.StoreResult(0)
	}
}

func ZPush(zm *ZMachine, args []uint16, numArgs uint16) {
	zm.stack.Push(args[0])
}

func ZPull(zm *ZMachine, args []uint16, numArgs uint16) {
	r := zm.stack.Pop()
	DebugPrintf("Popped %d 0x%X %d %d\n", r, zm.ip, numArgs, args[0])
	zm.StoreAtLocation(args[0], r)
}

func ZNOP_VAR(zm *ZMachine, args []uint16, numArgs uint16) {
	fmt.Printf("IP=0x%X\n", zm.ip)
	panic("NOP VAR")
}

func ZNOP(zm *ZMachine, args []uint16) {
	fmt.Printf("IP=0x%X\n", zm.ip)
	panic("NOP 2OP")
}

func GenericBranch(zm *ZMachine, conditionSatisfied bool) {
	branchInfo := zm.ReadByte()

	// "If bit 7 of the first byte is 0, a branch occurs when the condition was false; if 1, then branch is on true"
	branchOnFalse := (branchInfo >> 7) == 0

	var jumpAddress int32
	var branchOffset int32
	// 0 = return false, 1 = return true, 2 = standard jump
	returnFromCurrent := int32(2)
	// "If bit 6 is set, then the branch occupies 1 byte only, and the "offset" is in the range 0 to 63, given in the bottom 6 bits"
	if (branchInfo & (1 << 6)) != 0 {
		branchOffset = int32(branchInfo & 0x3F)

		// "An offset of 0 means "return false from the current routine", and 1 means "return true from the current routine".
		if branchOffset <= 1 {
			returnFromCurrent = branchOffset
		}
	} else {
		// If bit 6 is clear, then the offset is a signed 14-bit number given in bits 0 to 5 of the first
		// byte followed by all 8 of the second.
		secondPart := zm.ReadByte()
		firstPart := uint16(branchInfo & 0x3F)
		// Propagate sign bit (2 complement)
		if (firstPart & 0x20) != 0 {
			firstPart |= (1 << 6) | (1 << 7)
		}

		branchOffset16 := int16(firstPart<<8) | int16(secondPart)
		branchOffset = int32(branchOffset16)

		DebugPrintf("Offset: 0x%X [%d]\n", branchOffset, branchOffset)
	}
	ip := int32(zm.ip)

	// "Otherwise, a branch moves execution to the instruction at address
	// Address after branch data + Offset - 2."
	jumpAddress = ip + int32(branchOffset) - 2

	DebugPrintf("Jump address = 0x%X\n", jumpAddress)

	doJump := (conditionSatisfied != branchOnFalse)

	DebugPrintf("Do jump: %t\n", doJump)

	if doJump {
		if returnFromCurrent != 2 {
			ZRet(zm, uint16(returnFromCurrent))
		} else {
			zm.ip = uint32(jumpAddress)
		}
	}
}

func ZJumpEqual(zm *ZMachine, args []uint16, numArgs uint16) {

	conditionSatisfied := (args[0] == args[1] ||
		(numArgs > 2 && args[0] == args[2]) || (numArgs > 3 && args[0] == args[3]))
	GenericBranch(zm, conditionSatisfied)
}

func ZJumpLess(zm *ZMachine, args []uint16, numArgs uint16) {
	conditionSatisfied := int16(args[0]) < int16(args[1])
	GenericBranch(zm, conditionSatisfied)
}

func ZJumpGreater(zm *ZMachine, args []uint16, numArgs uint16) {
	conditionSatisfied := int16(args[0]) > int16(args[1])
	GenericBranch(zm, conditionSatisfied)
}

func ZAdd(zm *ZMachine, args []uint16, numArgs uint16) {
	r := int16(args[0]) + int16(args[1])
	zm.StoreResult(uint16(r))
}

func ZSub(zm *ZMachine, args []uint16, numArgs uint16) {
	r := int16(args[0]) - int16(args[1])
	zm.StoreResult(uint16(r))
}

func ZMul(zm *ZMachine, args []uint16, numArgs uint16) {
	r := int16(args[0]) * int16(args[1])
	zm.StoreResult(uint16(r))
}

func ZDiv(zm *ZMachine, args []uint16, numArgs uint16) {
	if args[1] == 0 {
		panic("Division by zero")
	}

	r := int16(args[0]) / int16(args[1])
	zm.StoreResult(uint16(r))
}

func ZMod(zm *ZMachine, args []uint16, numArgs uint16) {
	if args[1] == 0 {
		panic("Division by zero (mod)")
	}

	r := int16(args[0]) % int16(args[1])
	zm.StoreResult(uint16(r))
}

func ZStore(zm *ZMachine, args []uint16, numArgs uint16) {
	DebugPrintf("%d - 0x%X\n", args[0], args[1])
	zm.StoreAtLocation(args[0], args[1])
}

func ZTestAttr(zm *ZMachine, args []uint16, numArgs uint16) {
	GenericBranch(zm, zm.TestObjectAttr(args[0], args[1]))
}

func ZOr(zm *ZMachine, args []uint16, numArgs uint16) {
	zm.StoreResult(args[0] | args[1])
}

func ZAnd(zm *ZMachine, args []uint16, numArgs uint16) {
	zm.StoreResult(args[0] & args[1])
}

func ZSetAttr(zm *ZMachine, args []uint16, numArgs uint16) {
	zm.SetObjectAttr(args[0], args[1])
}

func ZClearAttr(zm *ZMachine, args []uint16, numArgs uint16) {
	zm.ClearObjectAttr(args[0], args[1])
}

func ZLoadB(zm *ZMachine, args []uint16, numArgs uint16) {

	address := args[0] + args[1]
	value := zm.buf[address]

	zm.StoreResult(uint16(value))
}

func ZGetProp(zm *ZMachine, args []uint16, numArgs uint16) {
	prop := zm.GetObjectProperty(args[0], args[1])
	zm.StoreResult(prop)
}

func ZGetPropAddr(zm *ZMachine, args []uint16, numArgs uint16) {
	addr := zm.GetObjectPropertyAddress(args[0], args[1])
	zm.StoreResult(addr)
}

func ZGetNextProp(zm *ZMachine, args []uint16, numArgs uint16) {
	addr := zm.GetNextObjectProperty(args[0], args[1])
	zm.StoreResult(addr)
}

// array word-index -> (result)
func ZLoadW(zm *ZMachine, args []uint16, numArgs uint16) {
	address := uint32(args[0] + (args[1] * 2))
	value := GetUint16(zm.buf, address)

	zm.StoreResult(value)
}

// dec_chk (variable) value ?(label)
// Decrement variable, and branch if it is now less than the given value.
func ZDecChk(zm *ZMachine, args []uint16, numArgs uint16) {
	newValue := zm.AddToVar(args[0], -1)
	GenericBranch(zm, int16(newValue) < int16(args[1]))
}

// inc_chk (variable) value ?(label)
// Increment variable, and branch if now greater than value.
func ZIncChk(zm *ZMachine, args []uint16, numArgs uint16) {
	newValue := zm.AddToVar(args[0], 1)
	GenericBranch(zm, int16(newValue) > int16(args[1]))
}

// test bitmap flags ?(label)
// Jump if all of the flags in bitmap are set (i.e. if bitmap & flags == flags).
func ZTest(zm *ZMachine, args []uint16, numArgs uint16) {
	bitmap := args[0]
	flags := args[1]
	GenericBranch(zm, (bitmap&flags) == flags)
}

//  jin obj1 obj2 ?(label)
// Jump if object a is a direct child of b, i.e., if parent of a is b.
func ZJin(zm *ZMachine, args []uint16, numArgs uint16) {
	GenericBranch(zm, zm.IsDirectParent(args[0], args[1]))
}

func ZInsertObj(zm *ZMachine, args []uint16, numArgs uint16) {
	zm.ReparentObject(args[0], args[1])
}

func ZJumpZero(zm *ZMachine, arg uint16) {
	GenericBranch(zm, arg == 0)
}

// get_sibling object -> (result) ?(label)
// Get next object in tree, branching if this exists, i.e. is not 0.
func ZGetSibling(zm *ZMachine, arg uint16) {
	sibling := zm.GetSibling(arg)
	zm.StoreResult(sibling)
	GenericBranch(zm, sibling != NULL_OBJECT_INDEX)
}

// get_child object -> (result) ?(label)
// Get first object contained in given object, branching if this exists, i.e. is not nothing (i.e., is not 0).
func ZGetChild(zm *ZMachine, arg uint16) {
	childIndex := zm.GetFirstChild(arg)
	zm.StoreResult(childIndex)
	GenericBranch(zm, childIndex != NULL_OBJECT_INDEX)
}

func ZGetParent(zm *ZMachine, arg uint16) {
	parent := zm.GetParentObject(arg)
	zm.StoreResult(parent)
}

func ZGetPropLen(zm *ZMachine, arg uint16) {
	if arg == 0 {
		zm.StoreResult(0)
	} else {
		// Arg = direct address of the property block
		// To get size, we need to go 1 byte back
		propSize := zm.buf[arg-1]
		numBytes := (propSize >> 5) + 1
		zm.StoreResult(uint16(numBytes))
	}
}

// print_paddr packed-address-of-string
func ZPrintPAddr(zm *ZMachine, arg uint16) {
	zm.DecodeZString(uint32(arg) * 2)
}

func ZLoad(zm *ZMachine, arg uint16) {
	zm.StoreResult(arg)
}

func ZInc(zm *ZMachine, arg uint16) {
	zm.AddToVar(arg, 1)
}

func ZDec(zm *ZMachine, arg uint16) {
	zm.AddToVar(arg, -1)
}

func ZPrintAddr(zm *ZMachine, arg uint16) {
	zm.DecodeZString(uint32(arg))
}

func ZRemoveObj(zm *ZMachine, arg uint16) {
	zm.UnlinkObject(arg)
}

func ZPrintObj(zm *ZMachine, arg uint16) {
	zm.PrintObjectName(arg)
}

func ZRet(zm *ZMachine, arg uint16) {
	returnAddress := zm.stack.RestoreFrame()
	zm.ip = returnAddress
	DebugPrintf("Returning to 0x%X\n", zm.ip)

	zm.StoreResult(arg)
}

// Unconditional jump
func ZJump(zm *ZMachine, arg uint16) {
	jumpOffset := int16(arg)
	jumpAddress := int32(zm.ip) + int32(jumpOffset) - 2
	DebugPrintf("Jump address: 0x%X\n", jumpAddress)
	zm.ip = uint32(jumpAddress)
}

func ZNOP1(zm *ZMachine, arg uint16) {
	fmt.Printf("IP=0x%X\n", zm.ip)
	panic("NOP1")
}

func ZReturnTrue(zm *ZMachine) {
	ZRet(zm, uint16(1))
}

func ZReturnFalse(zm *ZMachine) {
	ZRet(zm, uint16(0))
}

func ZPrint(zm *ZMachine) {
	zm.ip = zm.DecodeZString(zm.ip)
}

func ZPrintRet(zm *ZMachine) {
	zm.ip = zm.DecodeZString(zm.ip)
	fmt.Printf("\n")
	ZRet(zm, 1)
}

func ZRetPopped(zm *ZMachine) {
	retValue := zm.stack.Pop()
	ZRet(zm, retValue)
}

func ZPop(zm *ZMachine) {
	zm.stack.Pop()
}

func ZQuit(zm *ZMachine) {
	zm.Done = true
}

func ZNewLine(zm *ZMachine) {
	fmt.Printf("\n")
}

func ZNOP0(zm *ZMachine) {
	panic("NOP0")
}
