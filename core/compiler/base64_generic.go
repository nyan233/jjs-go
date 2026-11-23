//go:build !arm64

package compiler

import "encoding/base64"

func encodeBase64(dst, src []byte) { base64.StdEncoding.Encode(dst, src) }
