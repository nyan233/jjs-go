//go:build arm64_asm_test

package compiler

import (
	"encoding/binary"
	stdjson "encoding/json"
	"reflect"
	"testing"
)

type debugCustomValue struct {
	Value string
}

func (v debugCustomValue) MarshalJSON() ([]byte, error) {
	return stdjson.Marshal(v.Value)
}

type debugPointerOnlyValue struct {
	Value string
}

func (v *debugPointerOnlyValue) MarshalJSON() ([]byte, error) {
	return stdjson.Marshal(v.Value)
}

type debugNamedCodec interface {
	stdjson.Marshaler
	DebugName() string
}

type debugNamed struct {
	Value string
}

func (v *debugNamed) MarshalJSON() ([]byte, error) {
	return stdjson.Marshal(v.Value)
}

func (*debugNamed) DebugName() string {
	return "debug"
}

func TestDebugARM64CustomCode(t *testing.T) {
	typ := reflect.TypeOf(struct {
		Value debugCustomValue        `json:"value"`
		Ptr   *debugCustomValue       `json:"ptr"`
		Named debugNamedCodec         `json:"named"`
		OnlyP *debugPointerOnlyValue  `json:"only_ptr"`
		Raw   stdjson.RawMessage      `json:"raw"`
	}{})
	compiler := &jitEncoderCompiler{unitByType: make(map[jitEncoderUnitKey]int)}
	if _, err := compiler.unitIndex(typ, false); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < len(compiler.units); index++ {
		unit := compiler.units[index]
		generator := &jitEncoderGenerator{code: unit.code, compiler: compiler, addressable: unit.addressable}
		if err := generator.emitPrologue(); err != nil {
			t.Fatal(err)
		}
		if err := generator.emitValue(unit.typ, 0); err != nil {
			t.Fatal(err)
		}
		generator.emitReturn()
	}
	for index, unit := range compiler.units {
		text, err := unit.code.assemble()
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("unit %d %s addressable=%v bytes=%d", index, unit.typ, unit.addressable, len(text))
		for offset := 0x60; offset < len(text); offset += 4 {
			t.Logf("%03x: %08x", offset, binary.LittleEndian.Uint32(text[offset:]))
		}
	}
}
