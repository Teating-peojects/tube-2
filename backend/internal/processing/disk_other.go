//go:build !unix

package processing

import "math"

func freeBytes(string) (uint64, error) { return math.MaxUint64, nil }
