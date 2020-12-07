package randutil

import (
	"testing"
)

func TestFloat64(t *testing.T) {
	const total = 1 << 24
	for i := 0; i < total; i++ {
		n := Float64()
		if !(n >= 0 && n < 1) {
			t.Fatalf("out of range [0.0, 1.0): %f", n)
		}
	}
	// TODO: verify that distribution is uniform
}
