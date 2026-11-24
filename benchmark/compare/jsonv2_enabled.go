//go:build goexperiment.jsonv2

package compare

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
)

const jsonV2Available = true

var jsonV2CompatOptions = jsonv2.JoinOptions(
	jsonv2.FormatNilMapAsNull(true),
	jsonv2.FormatNilSliceAsNull(true),
	jsonv2.MatchCaseInsensitiveNames(true),
	jsontext.AllowInvalidUTF8(true),
)

func jsonV2Marshal(value any) ([]byte, error) {
	return jsonv2.Marshal(value, jsonV2CompatOptions)
}

func jsonV2Unmarshal(data []byte, target any) error {
	return jsonv2.Unmarshal(data, target, jsonV2CompatOptions)
}
