//go:build !unix

package worker

import "math"

func freeBytes(string) (uint64, error) { return math.MaxUint64, nil }
