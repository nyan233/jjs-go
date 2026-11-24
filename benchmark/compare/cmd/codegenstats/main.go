package main

import (
	"fmt"
	"reflect"

	"github.com/nyan233/jjs-go/benchmark/compare"
	"github.com/nyan233/jjs-go/core/compiler"
)

func main() {
	for _, fixture := range []struct {
		name  string
		value any
	}{
		{"Small", compare.SmallBook},
		{"Medium", compare.MediumValue},
		{"Large", compare.LargeValue},
	} {
		typ := reflect.TypeOf(fixture.value)
		encoder, err := compiler.EncoderCodegenStats(typ)
		if err != nil {
			panic(err)
		}
		decoder, err := compiler.DecoderCodegenStats(typ)
		if err != nil {
			panic(err)
		}
		fmt.Printf("%s %s encoder: %+v instruction-reduction=%.2f%%\n", fixture.name, typ, encoder, encoder.InstructionReduction())
		fmt.Printf("%s %s decoder: %+v instruction-reduction=%.2f%%\n", fixture.name, typ, decoder, decoder.InstructionReduction())
	}
}
