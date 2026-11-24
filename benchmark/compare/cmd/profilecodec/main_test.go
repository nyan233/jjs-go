package main

import "testing"

func TestWorkloadSemantics(t *testing.T) {
	for _, codec := range []string{"jjs", "sonic"} {
		for _, operation := range []string{"encode", "encode-reuse", "decode-reuse", "decode-fresh"} {
			for _, size := range []string{"Small", "Medium", "Large"} {
				t.Run(codec+"/"+operation+"/"+size, func(t *testing.T) {
					w, err := selectWorkload(codec, operation, size)
					if err != nil {
						t.Fatal(err)
					}
					for range 2 {
						if err := w.run(); err != nil {
							t.Fatal(err)
						}
						if err := w.check(); err != nil {
							t.Fatal(err)
						}
					}
				})
			}
		}
	}
}
