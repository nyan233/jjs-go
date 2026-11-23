package main

import (
	"fmt"
	"reflect"

	"github.com/nyan233/jjs-go/benchmark/compare"
	"github.com/nyan233/jjs-go/core/compiler"
)

func main() {
	for _, typ := range []reflect.Type{reflect.TypeOf(compare.SmallBook), reflect.TypeOf(compare.TwitterResponse{})} {
		report(typ)
	}
}

func report(typ reflect.Type) {
	encoder, err := compiler.EncoderCodegenStats(typ)
	if err != nil {
		panic(err)
	}
	decoder, err := compiler.DecoderCodegenStats(typ)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s encoder: %+v instruction-reduction=%.2f%%\n", typ, encoder, encoder.InstructionReduction())
	fmt.Printf("%s decoder: %+v instruction-reduction=%.2f%%\n", typ, decoder, decoder.InstructionReduction())
}
