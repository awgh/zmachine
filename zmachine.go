package zmachine

// based on: http://msinilo.pl/blog2/post/p1252/

import (
	"fmt"
	"strings"
)

type ZMachine struct {
	ip         uint32
	header     ZHeader
	buf        []uint8
	stack      *ZStack
	localFrame uint16
	Done       bool
}

// Doesn't modify IP
func (zm *ZMachine) PeekByte() uint8 {
	return zm.buf[zm.ip]
}

// Reads & moves to the next one (advances IP)
func (zm *ZMachine) ReadByte() uint8 {
	zm.ip++
	return zm.buf[zm.ip-1]
}

// Reads 2 bytes and advances IP
func (zm *ZMachine) ReadUint16() uint16 {
	retVal := zm.GetUint16(zm.ip)
	zm.ip += 2
	return retVal
}

// We can only write to dynamic memory
func (zm *ZMachine) IsSafeToWrite(address uint32) bool {
	return address < zm.header.staticMemAddress
}

func (zm *ZMachine) GetUint16(offset uint32) uint16 {
	return (uint16(zm.buf[offset]) << 8) | (uint16)(zm.buf[offset+1])
}

func (zm *ZMachine) SetUint16(offset uint32, v uint16) {
	zm.buf[offset] = uint8(v >> 8)
	zm.buf[offset+1] = uint8(v & 0xFF)
}

func (zm *ZMachine) ReadGlobal(x uint8) uint16 {
	if x < 0x10 {
		panic("Invalid global variable")
	}

	addr := PackedAddress(uint32(x) - 0x10)
	ret := zm.GetUint16(zm.header.globalVarAddress + addr)

	return ret
}

func (zm *ZMachine) SetGlobal(x uint16, v uint16) {
	if x < 0x10 {
		panic("Invalid global variable")
	}

	addr := PackedAddress(uint32(x) - 0x10)
	zm.SetUint16(zm.header.globalVarAddress+addr, v)
}

func (zm *ZMachine) GetObjectEntryAddress(objectIndex uint16) uint32 {
	if objectIndex > MAX_OBJECT || objectIndex == 0 {
		fmt.Printf("Index: %d\n", objectIndex)
		panic("Invalid object index")
	}

	// Convert from 1-based (0 = NULL = no object) to 0-based

	objectIndex--
	// Skip default props
	objectEntryAddress := zm.header.objTableAddress + (31 * 2) + uint32(objectIndex*OBJECT_ENTRY_SIZE)

	return uint32(objectEntryAddress)
}

func (zm *ZMachine) SetObjectProperty(objectIndex uint16, propertyId uint16, value uint16) {

	objectEntryAddress := uint32(zm.GetObjectEntryAddress(objectIndex))

	propertiesAddress := GetUint16(zm.buf, objectEntryAddress+7)
	nameLength := uint16(zm.buf[propertiesAddress]) * 2 // in 2-byte words

	// Find property
	found := false
	propData := uint32(propertiesAddress + nameLength + 1)

	for !found {
		propSize := zm.buf[propData]
		if propSize == 0 {
			break
		}
		propData++
		propNo := uint16(propSize & 0x1F)

		// Props are sorted
		if propNo < propertyId {
			break
		}

		numBytes := (propSize >> 5) + 1
		if propNo == propertyId {
			found = true

			if numBytes == 1 {
				zm.buf[propData] = uint8(value & 0xFF)
			} else if numBytes == 2 {
				zm.SetUint16(propData, value)
			} else {
				panic("SetObjectProperty only supports 1/2 byte properties")
			}
		}
		propData += uint32(numBytes)
	}
	if !found {
		panic("Property not found!")
	}
}

func (zm *ZMachine) GetFirstPropertyAddress(objectIndex uint16) uint16 {
	objectEntryAddress := uint32(zm.GetObjectEntryAddress(objectIndex))
	propertiesAddress := GetUint16(zm.buf, objectEntryAddress+7)
	nameLength := uint16(zm.buf[propertiesAddress]) * 2 // in 2-byte words
	propData := propertiesAddress + nameLength + 1

	return propData
}

// Returns prop data address, number of property bytes
// (0 if not found)
func (zm *ZMachine) GetObjectPropertyInfo(objectIndex uint16, propertyId uint16) (uint16, uint16) {

	propData := zm.GetFirstPropertyAddress(objectIndex)

	// Find property
	found := false

	for !found {
		propSize := zm.buf[propData]
		if propSize == 0 {
			break
		}
		propData++
		propNo := uint16(propSize & 0x1F)

		// Props are sorted
		if propNo < propertyId {
			break
		}

		numBytes := uint16(propSize>>5) + 1
		if propNo == propertyId {
			return propData, numBytes
		}
		propData += uint16(numBytes)
	}
	return uint16(0), uint16(0)
}

func (zm *ZMachine) GetObjectPropertyAddress(objectIndex uint16, propertyId uint16) uint16 {
	address, _ := zm.GetObjectPropertyInfo(objectIndex, propertyId)
	return address
}

func (zm *ZMachine) GetNextObjectProperty(objectIndex uint16, propertyId uint16) uint16 {

	nextPropSize := uint8(0)

	// " if called with zero, it gives the first property number present."
	if propertyId == 0 {
		propData := zm.GetFirstPropertyAddress(objectIndex)
		nextPropSize = zm.buf[propData]
	} else {
		propData, numBytes := zm.GetObjectPropertyInfo(objectIndex, propertyId)
		if propData == 0 {
			panic("GetNextObjectProperty - non existent property")
		}
		nextPropSize = zm.buf[propData+numBytes]
	}
	// "zero, indicating the end of the property list"
	if nextPropSize == 0 {
		return 0
	} else {
		return uint16(nextPropSize & 0x1F)
	}
}

func (zm *ZMachine) GetObjectProperty(objectIndex uint16, propertyId uint16) uint16 {

	propData, numBytes := zm.GetObjectPropertyInfo(objectIndex, propertyId)
	result := uint16(0)

	if propData == 0 {
		// Get a default one
		result = zm.GetPropertyDefault(propertyId)
		DebugPrintf("Default prop %d = 0x%X\n", propertyId, result)
	} else {
		if numBytes == 1 {
			result = uint16(zm.buf[propData])
		} else if numBytes == 2 {
			result = GetUint16(zm.buf, uint32(propData))
		} else {
			panic("GetObjectProperty only supports 1/2 byte properties")
		}
	}

	return result
}

// True if set
func (zm *ZMachine) TestObjectAttr(objectIndex uint16, attribute uint16) bool {

	if attribute > 31 {
		panic("Attribute out of bounds")
	}

	objectEntryAddress := zm.GetObjectEntryAddress(objectIndex)

	attribs := GetUint32(zm.buf, objectEntryAddress)
	// 0: top bit
	// 31: bottom bit
	mask := uint32(1 << (31 - attribute))

	return (attribs & mask) != 0
}

func (zm *ZMachine) SetObjectAttr(objectIndex uint16, attribute uint16) {

	if attribute > 31 {
		panic("Attribute out of bounds")
	}

	objectEntryAddress := zm.GetObjectEntryAddress(objectIndex)
	byteIndex := uint32(attribute >> 3)
	shift := 7 - (attribute & 0x7)

	zm.buf[objectEntryAddress+byteIndex] |= (1 << shift)
}

func (zm *ZMachine) ClearObjectAttr(objectIndex uint16, attribute uint16) {

	if attribute > 31 {
		panic("Attribute out of bounds")
	}

	objectEntryAddress := zm.GetObjectEntryAddress(objectIndex)
	byteIndex := uint32(attribute >> 3)
	shift := 7 - (attribute & 0x7)

	zm.buf[objectEntryAddress+byteIndex] &= ^(1 << shift)
}

func (zm *ZMachine) IsDirectParent(childIndex uint16, parentIndex uint16) bool {

	return zm.GetParentObject(childIndex) == parentIndex
}

func (zm *ZMachine) GetParentObject(objectIndex uint16) uint16 {
	objectEntryAddress := zm.GetObjectEntryAddress(objectIndex)

	return uint16(zm.buf[objectEntryAddress+OBJECT_PARENT_INDEX])
}

// Unlink object from its parent
func (zm *ZMachine) UnlinkObject(objectIndex uint16) {
	objectEntryAddress := zm.GetObjectEntryAddress(objectIndex)
	currentParentIndex := uint16(zm.buf[objectEntryAddress+OBJECT_PARENT_INDEX])

	// Unlink from current parent first
	if currentParentIndex != NULL_OBJECT_INDEX {
		curParentAddress := zm.GetObjectEntryAddress(currentParentIndex)
		// If we're the first child -> move to sibling
		if uint16(zm.buf[curParentAddress+OBJECT_CHILD_INDEX]) == objectIndex {
			zm.buf[curParentAddress+OBJECT_CHILD_INDEX] = zm.buf[objectEntryAddress+OBJECT_SIBLING_INDEX]
		} else {
			childIter := uint16(zm.buf[curParentAddress+OBJECT_CHILD_INDEX])
			prevChild := uint16(NULL_OBJECT_INDEX)
			for childIter != objectIndex && childIter != NULL_OBJECT_INDEX {
				prevChild = childIter
				childIter = zm.GetSibling(childIter)
			}
			// Sanity checks
			if childIter == NULL_OBJECT_INDEX {
				panic("Object not found on parent children list")
			}
			if prevChild == NULL_OBJECT_INDEX {
				panic("Corrupted data")
			}

			prevSiblingAddress := zm.GetObjectEntryAddress(prevChild)
			sibling := zm.buf[objectEntryAddress+OBJECT_SIBLING_INDEX]
			zm.buf[prevSiblingAddress+OBJECT_SIBLING_INDEX] = sibling
		}
		zm.buf[objectEntryAddress+OBJECT_PARENT_INDEX] = NULL_OBJECT_INDEX
	}
}

func (zm *ZMachine) ReparentObject(objectIndex uint16, newParentIndex uint16) {

	objectEntryAddress := zm.GetObjectEntryAddress(objectIndex)
	currentParentIndex := uint16(zm.buf[objectEntryAddress+OBJECT_PARENT_INDEX])

	if currentParentIndex == newParentIndex {
		return
	}

	zm.UnlinkObject(objectIndex)

	// Make the first child of our new parent
	newParentAddress := zm.GetObjectEntryAddress(newParentIndex)
	zm.buf[objectEntryAddress+OBJECT_SIBLING_INDEX] = zm.buf[newParentAddress+OBJECT_CHILD_INDEX]
	zm.buf[newParentAddress+OBJECT_CHILD_INDEX] = uint8(objectIndex)
	zm.buf[objectEntryAddress+OBJECT_PARENT_INDEX] = uint8(newParentIndex)
}

func (zm *ZMachine) GetFirstChild(objectIndex uint16) uint16 {
	objectEntryAddress := zm.GetObjectEntryAddress(objectIndex)

	return uint16(zm.buf[objectEntryAddress+OBJECT_CHILD_INDEX])
}

func (zm *ZMachine) GetSibling(objectIndex uint16) uint16 {
	objectEntryAddress := zm.GetObjectEntryAddress(objectIndex)

	return uint16(zm.buf[objectEntryAddress+OBJECT_SIBLING_INDEX])
}

func (zm *ZMachine) PrintObjectName(objectIndex uint16) {
	objectEntryAddress := zm.GetObjectEntryAddress(objectIndex)
	propertiesAddress := uint32(GetUint16(zm.buf, objectEntryAddress+7))
	zm.DecodeZString(propertiesAddress + 1)
}

// Returns new value.
func (zm *ZMachine) AddToVar(varType uint16, value int16) uint16 {
	retValue := uint16(0)
	if varType == 0 {
		zm.stack.stack[zm.stack.top] += uint16(value)
		retValue = zm.stack.GetTopItem()
	} else if varType < 0x10 {
		retValue = zm.stack.GetLocalVar((int)(varType - 1))
		retValue += uint16(value)
		zm.stack.SetLocalVar(int(varType-1), retValue)
	} else {
		retValue = zm.ReadGlobal(uint8(varType))
		retValue += uint16(value)
		zm.SetGlobal(varType, retValue)
	}
	return retValue
}

func (zm *ZMachine) GetOperand(operandType byte) uint16 {

	var retValue uint16

	switch operandType {
	case OPERAND_SMALL:
		retValue = uint16(zm.buf[zm.ip])
		zm.ip++
	case OPERAND_VARIABLE:
		varType := zm.buf[zm.ip]
		// 0 = top of the stack
		// 1 - 0xF = locals
		// 0x10 - 0xFF = globals
		if varType == 0 {
			retValue = zm.stack.Pop()
		} else if varType < 0x10 {
			retValue = zm.stack.GetLocalVar((int)(varType - 1))
		} else {
			retValue = zm.ReadGlobal(varType)
		}
		zm.ip++
	case OPERAND_LARGE:
		retValue = GetUint16(zm.buf, zm.ip)
		zm.ip += 2
	case OPERAND_OMITTED:
		return 0
	default:
		panic("Unknown operand type")
	}

	return retValue
}

func (zm *ZMachine) GetOperands(opTypesByte uint8, operandValues []uint16) uint16 {
	numOperands := uint16(0)
	var shift uint8
	shift = 6

	for i := 0; i < 4; i++ {
		opType := (opTypesByte >> shift) & 0x3
		shift -= 2
		if opType == OPERAND_OMITTED {
			break
		}

		opValue := zm.GetOperand(opType)
		operandValues[numOperands] = opValue
		numOperands++
	}

	return numOperands
}

func (zm *ZMachine) StoreAtLocation(storeLocation uint16, v uint16) {
	// Same deal as read variable
	// 0 = top of the stack, 0x1-0xF = local var, 0x10 - 0xFF = global var

	if storeLocation == 0 {
		zm.stack.Push(v)
	} else if storeLocation < 0x10 {
		zm.stack.SetLocalVar((int)(storeLocation-1), v)
	} else {
		zm.SetGlobal(storeLocation, v)
	}
}

func (zm *ZMachine) StoreResult(v uint16) {
	storeLocation := zm.ReadByte()

	zm.StoreAtLocation(uint16(storeLocation), v)
}

func (zm *ZMachine) InterpretVARInstruction() {

	opcode := zm.ReadByte()
	// "In variable form, if bit 5 is 0 then the count is 2OP; if it is 1, then the count is VAR.
	// The opcode number is given in the bottom 5 bits.
	instruction := (opcode & 0x1F)
	twoOp := ((opcode >> 5) & 0x1) == 0

	// "In variable or extended forms, a byte of 4 operand types is given next.
	// This contains 4 2-bit fields: bits 6 and 7 are the first field, bits 0 and 1 the fourth."
	// "A value of 0 means a small constant and 1 means a variable."
	opTypesByte := zm.ReadByte()

	opValues := make([]uint16, 4)
	numOperands := zm.GetOperands(opTypesByte, opValues)

	if twoOp {
		fn := ZFunctions_2OP[instruction]
		fn(zm, opValues, numOperands)
	} else {
		fn := ZFunctions_VAR[instruction]
		fn(zm, opValues, numOperands)
	}
}

func (zm *ZMachine) InterpretShortInstruction() {
	// "In short form, bits 4 and 5 of the opcode byte give an operand type.
	// If this is $11 then the operand count is 0OP; otherwise, 1OP. In either case the opcode number is given in the bottom 4 bits."

	opcode := zm.ReadByte()
	opType := (opcode >> 4) & 0x3
	instruction := (opcode & 0x0F)

	if opType != OPERAND_OMITTED {
		opValue := zm.GetOperand(opType)

		fn := ZFunctions_1OP[instruction]
		fn(zm, opValue)
	} else {
		fn := ZFunctions_0P[instruction]
		fn(zm)
	}
}

func (zm *ZMachine) InterpretLongInstruction() {

	opcode := zm.ReadByte()

	// In long form the operand count is always 2OP. The opcode number is given in the bottom 5 bits.
	instruction := (opcode & 0x1F)

	// Operand types:
	// In long form, bit 6 of the opcode gives the type of the first operand, bit 5 of the second.
	// A value of 0 means a small constant and 1 means a variable.
	operandType0 := ((opcode & 0x40) >> 6) + 1
	operandType1 := ((opcode & 0x20) >> 5) + 1

	opValues := make([]uint16, 2)
	opValue0 := zm.GetOperand(operandType0)
	opValue1 := zm.GetOperand(operandType1)

	opValues[0] = opValue0
	opValues[1] = opValue1

	fn := ZFunctions_2OP[instruction]
	fn(zm, opValues, 2)
}

func (zm *ZMachine) InterpretInstruction() {
	opcode := zm.PeekByte()

	DebugPrintf("IP: 0x%X - opcode: 0x%X\n", zm.ip, opcode)
	// Form is stored in top 2 bits
	// "If the top two bits of the opcode are $$11 the form is variable; if $$10, the form is short.
	// If the opcode is 190 ($BE in hexadecimal) and the version is 5 or later, the form is "extended".
	// Otherwise, the form is "long"."
	form := (opcode >> 6) & 0x3

	if form == 0x2 {
		zm.InterpretShortInstruction()
	} else if form == 0x3 {
		zm.InterpretVARInstruction()
	} else {
		zm.InterpretLongInstruction()
	}
}

// NOTE: Doesn't support abbreviations.
func (zm *ZMachine) EncodeText(txt string) uint32 {

	encodedChars := make([]uint8, 12)
	encodedWords := make([]uint16, 2)
	padding := uint8(0x5)

	// Store 6 Z-chars. Clamp if longer, add padding if shorter
	i := 0
	j := 0
	for i < 6 {
		if j < len(txt) {
			c := txt[j]
			j++

			// See if we can find any alphabet
			ai := -1
			alphabetType := 0
			for a := 0; a < len(alphabets); a++ {
				index := strings.IndexByte(alphabets[a], c)
				if index >= 0 {
					ai = index
					alphabetType = a
					break
				}
			}
			if ai >= 0 {
				if alphabetType != 0 {
					// Alphabet change
					encodedChars[i] = uint8(alphabetType + 3)
					encodedChars[i+1] = uint8(ai + 6)
					i += 2
				} else {
					encodedChars[i] = uint8(ai + 6)
					i++
				}
			} else {
				// 10-bit ZC
				encodedChars[i] = 5
				encodedChars[i+1] = 6
				encodedChars[i+2] = (c >> 5)
				encodedChars[i+3] = (c & 0x1F)
				i += 4
			}
		} else {
			// Padding
			encodedChars[i] = padding
			i++
		}
	}

	for i := 0; i < 2; i++ {
		encodedWords[i] = (uint16(encodedChars[i*3+0]) << 10) | (uint16(encodedChars[i*3+1]) << 5) |
			uint16(encodedChars[i*3+2])
		if i == 1 {
			encodedWords[i] |= 0x8000
		}
	}

	return (uint32(encodedWords[0]) << 16) | uint32(encodedWords[1])
}

func (zm *ZMachine) Initialize(buffer []uint8, header ZHeader) {
	zm.buf = buffer
	zm.header = header
	zm.ip = uint32(header.ip)
	zm.stack = NewStack()

	//zm.TestDictionary()
}

// Return DICT_NOT_FOUND (= 0) if not found
// Address in dictionary otherwise
func (zm *ZMachine) FindInDictionary(str string) uint16 {

	numSeparators := uint32(zm.buf[zm.header.dictAddress])
	entryLength := uint16(zm.buf[zm.header.dictAddress+1+numSeparators])
	numEntries := GetUint16(zm.buf, zm.header.dictAddress+1+numSeparators+1)

	entriesAddress := zm.header.dictAddress + 1 + numSeparators + 1 + 2

	// Dictionary entries are sorted, so we can use binary search
	lowerBound := uint16(0)
	upperBound := numEntries - 1

	encodedText := zm.EncodeText(str)

	foundAddress := uint16(DICT_NOT_FOUND)
	for lowerBound <= upperBound {

		currentIndex := lowerBound + (upperBound-lowerBound)/2
		dictValue := GetUint32(zm.buf, entriesAddress+uint32(currentIndex*entryLength))

		if encodedText < dictValue {
			upperBound = currentIndex - 1
		} else if encodedText > dictValue {
			lowerBound = currentIndex + 1
		} else {
			foundAddress = uint16(entriesAddress + uint32(currentIndex*entryLength))
			break
		}
	}

	return foundAddress
}

func (zm *ZMachine) GetPropertyDefault(propertyIndex uint16) uint16 {
	if propertyIndex < 1 || propertyIndex > 31 {
		panic("Invalid propertyIndex")
	}

	// 1-based -> 0-based
	propertyIndex--
	return GetUint16(zm.buf, zm.header.objTableAddress+uint32(propertyIndex*2))
}

// V3 only
// Returns offset pointing just after the string data
func (zm *ZMachine) DecodeZString(startOffset uint32) uint32 {

	done := false
	zchars := []uint8{}

	i := startOffset
	for !done {

		//--first byte-------   --second byte---
		//7    6 5 4 3 2  1 0   7 6 5  4 3 2 1 0
		//bit  --first--  --second---  --third--

		w16 := GetUint16(zm.buf, i)

		done = (w16 & 0x8000) != 0
		zchars = append(zchars, uint8((w16>>10)&0x1F), uint8((w16>>5)&0x1F), uint8(w16&0x1F))

		i += 2
	}

	alphabetType := 0

	for i := 0; i < len(zchars); i++ {
		zc := zchars[i]

		// Abbreviation
		if zc > 0 && zc < 4 {
			abbrevIndex := zchars[i+1]

			// "If z is the first Z-character (1, 2 or 3) and x the subsequent one,
			// then the interpreter must look up entry 32(z-1)+x in the abbreviations table"
			abbrevAddress := GetUint16(zm.buf, zm.header.abbreviationTable+uint32(32*(zc-1)+abbrevIndex)*2)
			zm.DecodeZString(PackedAddress(uint32(abbrevAddress)))

			alphabetType = 0
			i++
			continue
		}
		if zc == 4 {
			alphabetType = 1
			continue
		} else if zc == 5 {
			alphabetType = 2
			continue
		}

		// Z-character 6 from A2 means that the two subsequent Z-characters specify a ten-bit ZSCII character code:
		// the next Z-character gives the top 5 bits and the one after the bottom 5.
		if alphabetType == 2 && zc == 6 {

			zc10 := (uint16(zchars[i+1]) << 5) | uint16(zchars[i+2])
			PrintZChar(zc10)

			i += 2

			alphabetType = 0
			continue
		}

		if zc == 0 {
			fmt.Printf(" ")
		} else {
			// If we're here zc >= 6. Alphabet tables are indexed starting at 6
			aindex := zc - 6
			fmt.Printf("%c", alphabets[alphabetType][aindex])
		}

		alphabetType = 0
	}

	return i
}
