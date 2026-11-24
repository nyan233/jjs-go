//go:build !goexperiment.jsonv2

package compare

const jsonV2Available = false

func jsonV2Marshal(any) ([]byte, error) {
	panic("encoding/json/v2 benchmark requires GOEXPERIMENT=jsonv2")
}

func jsonV2Unmarshal([]byte, any) error {
	panic("encoding/json/v2 benchmark requires GOEXPERIMENT=jsonv2")
}
