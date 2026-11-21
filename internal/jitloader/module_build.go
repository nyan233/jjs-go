package jitloader

import (
	"encoding/binary"
	"reflect"
	"unsafe"
)

const (
	funcDataArgsPointerMaps   = 0
	funcDataLocalsPointerMaps = 1

	pcDataUnsafePoint   = 0
	pcDataStackMapIndex = 1

	unsafePointUnsafe = -2
)

type stackMap struct {
	n        int32
	nbit     int32
	byteData [1]byte
}

func buildModule(spec moduleSpec) loadedModule {
	textBytes, textAddress := mmapExecutable(spec.text)

	funcName := loadedFunctionName(spec)
	funcNames := append([]byte{0}, funcName...)
	funcNames = append(funcNames, 0)
	fileName := spec.loader.File
	fileTable := append([]byte(fileName), 0)
	cuTable := []uint32{0}

	pcTable := []byte{0}
	appendPC := func(data Pcdata) uint32 {
		if len(data) == 0 {
			return 0
		}
		offset := uint32(len(pcTable))
		pcTable = append(pcTable, encodePCData(data)...)
		return offset
	}
	pcUnsafe := appendPC(Pcdata{{PC: uint32(len(spec.text)), Val: unsafePointUnsafe}})
	pcStackMap := appendPC(Pcdata{{PC: uint32(len(spec.text)), Val: 0}})
	pcsp := appendPC(spec.pcsp)

	argMap := makeStackMap(spec.argPtrs)
	localMap := makeStackMap(spec.localPtrs)
	funcData := make([]byte, 0, len(argMap)+len(localMap))
	argMapOffset := uint32(len(funcData))
	funcData = append(funcData, argMap...)
	localMapOffset := uint32(len(funcData))
	funcData = append(funcData, localMap...)

	info := funcInfo{
		entryOff:  0,
		nameOff:   int32(1),
		args:      int32(spec.argSize),
		pcsp:      pcsp,
		npcdata:   nPCDATA,
		startLn:   1,
		nfuncdata: nFUNCDATA,
	}
	record := make([]byte, int(unsafe.Sizeof(info)))
	*(*funcInfo)(unsafe.Pointer(&record[0])) = info
	for _, offset := range []uint32{pcUnsafe, pcStackMap, 0, 0} {
		record = binary.LittleEndian.AppendUint32(record, offset)
	}
	funcDataOffsets := make([]uint32, nFUNCDATA)
	for i := range funcDataOffsets {
		funcDataOffsets[i] = pcdatInvalid
	}
	funcDataOffsets[funcDataArgsPointerMaps] = argMapOffset
	funcDataOffsets[funcDataLocalsPointerMaps] = localMapOffset
	for _, offset := range funcDataOffsets {
		record = binary.LittleEndian.AppendUint32(record, offset)
	}

	funcTable := []functab{
		{entryOff: 0, funcOff: 0},
		{entryOff: uint32(len(spec.text))},
	}
	findFunc := make([]findfuncbucket, (len(spec.text)+4095)/4096)
	findFuncBytes := unsafe.Slice(
		(*byte)(unsafe.Pointer(unsafe.SliceData(findFunc))),
		len(findFunc)*int(unsafe.Sizeof(findFunc[0])),
	)

	module := new(moduledata)
	module.funcnametab = funcNames
	module.cutab = cuTable
	module.filetab = fileTable
	module.pctab = pcTable
	module.pclntable = record
	module.ftab = funcTable
	module.findfunctab = uintptr(unsafe.Pointer(unsafe.SliceData(findFunc)))
	module.minpc = textAddress
	module.maxpc = textAddress + uintptr(len(spec.text))
	module.text = textAddress
	module.etext = textAddress + uintptr(len(textBytes))
	module.gofunc = uintptr(unsafe.Pointer(unsafe.SliceData(funcData)))
	module.gcdata = uintptr(unsafe.Pointer(&emptyGCData))
	module.gcbss = uintptr(unsafe.Pointer(&emptyGCData))
	module.modulename = spec.loader.Name + spec.name
	setEpclntab(module, uintptr(unsafe.Pointer(unsafe.SliceData(record)))+uintptr(len(record)))
	module.pcHeader = &pcHeader{
		magic:    pcLnTabMagic,
		minLC:    uint8(pcQuantum),
		ptrSize:  uint8(unsafe.Sizeof(uintptr(0))),
		nfunc:    1,
		nfiles:   1,
		funcname: fieldOffset("funcnametab"),
		cu:       fieldOffset("cutab"),
		filetab:  fieldOffset("filetab"),
		pctab:    fieldOffset("pctab"),
		pcln:     fieldOffset("pclntable"),
	}

	retain := moduleRetain{
		text:        textBytes,
		funcNames:   funcNames,
		cuTable:     cuTable,
		fileTable:   fileTable,
		pcTable:     pcTable,
		pcLineTable: findFuncBytes,
		funcTable:   funcTable,
		findFunc:    findFuncBytes,
		funcData:    funcData,
	}
	putModuleRetain(module, retain)
	return loadedModule{metadata: module, retain: retain}
}

func makeStackMap(flags []bool) []byte {
	byteCount := (len(flags) + 7) / 8
	size := int(unsafe.Sizeof(stackMap{})) - 1 + byteCount
	data := make([]byte, size)
	*(*int32)(unsafe.Pointer(&data[0])) = 1
	*(*int32)(unsafe.Pointer(&data[4])) = int32(len(flags))
	for i, isPointer := range flags {
		if isPointer {
			data[8+i/8] |= 1 << uint(i%8)
		}
	}
	return data
}

func encodePCData(data Pcdata) []byte {
	var encoded []byte
	var scratch [binary.MaxVarintLen32]byte
	previousValue := int32(-1)
	var previousPC uint32
	for _, value := range data {
		if value.PC < previousPC {
			panic("jitloader: PC values must be sorted")
		}
		pcDelta := (value.PC - previousPC) / pcQuantum
		valueDelta := int64(value.Val - previousValue)
		if pcDelta == 0 || valueDelta == 0 {
			continue
		}
		n := binary.PutVarint(scratch[:], valueDelta)
		encoded = append(encoded, scratch[:n]...)
		n = binary.PutUvarint(scratch[:], uint64(pcDelta))
		encoded = append(encoded, scratch[:n]...)
		previousPC = value.PC
		previousValue = value.Val
	}
	return append(encoded, 0)
}

func fieldOffset(name string) uintptr {
	field, ok := reflect.TypeOf(moduledata{}).FieldByName(name)
	if !ok {
		panic("jitloader: moduledata field missing: " + name)
	}
	return field.Offset
}
