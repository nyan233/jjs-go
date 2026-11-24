//go:build !darwin || !cgo

package main

import "errors"

func requestWorkerQoS(string) (map[string]int, error) {
	return nil, errors.New("worker QoS requires macOS and cgo")
}
