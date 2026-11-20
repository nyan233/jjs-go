package main

import (
	"fmt"
	"reflect"

	"github.com/nyan233/jjs-go/benchmark/compare"
	"github.com/nyan233/jjs-go/core/compiler"
)

func main() {
	typ := reflect.TypeOf(compare.TwitterResponse{})
	encoder, err := compiler.EncoderCodegenStats(typ)
	if err != nil {
		panic(err)
	}
	decoder, err := compiler.DecoderCodegenStats(typ)
	if err != nil {
		panic(err)
	}
	fmt.Printf("encoder: %+v instruction-reduction=%.2f%%\n", encoder, encoder.InstructionReduction())
	fmt.Printf("decoder: %+v instruction-reduction=%.2f%%\n", decoder, decoder.InstructionReduction())
}
