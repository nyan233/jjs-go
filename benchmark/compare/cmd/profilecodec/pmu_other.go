//go:build !darwin || !cgo

package main

import "errors"

type pmuSession struct{ names [5]string }

func openPMU(string) (*pmuSession, error) {
	return nil, errors.New("PMU worker counters require macOS and cgo")
}

func (*pmuSession) sample() ([5]uint64, error) { return [5]uint64{}, nil }
func (*pmuSession) close() error               { return nil }
