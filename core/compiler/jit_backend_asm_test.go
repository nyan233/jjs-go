//go:build arm64 || arm64_asm_test

package compiler

import (
	"bytes"
	"reflect"
	"testing"
	"unsafe"
)

func TestARM64JITAssembly(t *testing.T) {
	code := newJITCodeBuilder("arm64_test")
	code.functionPrologue(64, int64(unsafe.Offsetof(jitEncodeContext{}.data)))
	code.movConst(7, _R11)
	code.store8(_R11, mem(_RSP, 32))
	code.loadImm64(0x1122334455667788)
	code.store32(_R11, mem(_RSP, 40))
	code.vectorMove16(mem(_RSP, 0), mem(_RSP, 32))
	code.call(_R11)
	code.restoreJITRegs()
	code.finishFunction(64)

	text, err := code.assemble()
	if err != nil {
		t.Fatal(err)
	}
	if len(text) == 0 || len(text)%4 != 0 {
		t.Fatalf("invalid ARM64 text size %d", len(text))
	}
	for _, instruction := range [][]byte{
		{0xfe, 0x0f, 0x1c, 0xf8}, // STR X30, [SP, #-64]!
		{0xe5, 0x00, 0x80, 0xd2}, // MOVZ X5, #7
		{0xa0, 0x00, 0x3f, 0xd6}, // BLR X5
		{0xc0, 0x03, 0x5f, 0xd6}, // RET
	} {
		if !bytes.Contains(text, instruction) {
			t.Fatalf("missing ARM64 instruction %x in %x", instruction, text)
		}
	}
	if code.stats.CodeBytes != len(text) || code.stats.InstructionsAfter == 0 {
		t.Fatalf("unexpected codegen stats: %+v", code.stats)
	}
}

func TestARM64EncoderAssembly(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeOf(TestJITNested{}),
		reflect.TypeOf(&TestJITPointer{}),
	} {
		compiler := &jitEncoderCompiler{unitByType: make(map[jitEncoderUnitKey]int)}
		rootType := typ
		if rootType.Kind() == reflect.Pointer {
			rootType = rootType.Elem()
		}
		if _, err := compiler.unitIndex(rootType, typ.Kind() == reflect.Pointer); err != nil {
			t.Fatal(err)
		}
		for index := 0; index < len(compiler.units); index++ {
			unit := compiler.units[index]
			generator := &jitEncoderGenerator{
				code:        unit.code,
				compiler:    compiler,
				addressable: unit.addressable,
			}
			if err := generator.emitPrologue(); err != nil {
				t.Fatal(err)
			}
			if err := generator.emitValue(unit.typ, 0); err != nil {
				t.Fatal(err)
			}
			generator.emitReturn()
		}
		for _, unit := range compiler.units {
			text, err := unit.code.assemble()
			if err != nil {
				t.Fatalf("%s: %v", typ, err)
			}
			if len(text) == 0 || !bytes.Contains(text, []byte{0xc0, 0x03, 0x5f, 0xd6}) {
				t.Fatalf("%s generated invalid ARM64 text", typ)
			}
		}
	}
}

func TestARM64StackAdjust(t *testing.T) {
	code := newJITCodeBuilder("arm64_stack")
	code.functionPrologue(64, int64(unsafe.Offsetof(jitEncodeContext{}.data)))
	code.finishFunction(64)
	text, err := code.assemble()
	if err != nil {
		t.Fatal(err)
	}
	if len(text) < 12 || !bytes.Equal(text[:4], []byte{0xfe, 0x0f, 0x1c, 0xf8}) {
		t.Fatalf("ARM64 frame prologue not assembled: %x", text)
	}
}
