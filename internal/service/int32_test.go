package service

import (
	"math"
	"testing"
)

func TestIntToInt32Clamped(t *testing.T) {
	for _, test := range []struct {
		name string
		input int
		want int32
	}{
		{"in range", 42, 42},
		{"max", math.MaxInt32, math.MaxInt32},
		{"above max", int(math.MaxInt32) + 1, math.MaxInt32},
		{"min", math.MinInt32, math.MinInt32},
		{"below min", int(math.MinInt32) - 1, math.MinInt32},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := intToInt32Clamped(test.input); got != test.want {
				t.Fatalf("intToInt32Clamped(%d) = %d, want %d", test.input, got, test.want)
			}
		})
	}
}

func TestNonNegativeInt32Clamping(t *testing.T) {
	if got := nonNegativeIntToInt32Clamped(-1); got != 0 {
		t.Fatalf("negative int = %d, want 0", got)
	}
	if got := nonNegativeIntToInt32Clamped(1); got != 1 {
		t.Fatalf("positive int = %d, want 1", got)
	}
	if got := nonNegativeIntToInt32Clamped(int(math.MaxInt32) + 1); got != math.MaxInt32 {
		t.Fatalf("large int = %d, want max", got)
	}
	if got := nonNegativeInt64ToInt32Clamped(-1); got != 0 {
		t.Fatalf("negative int64 = %d, want 0", got)
	}
	if got := nonNegativeInt64ToInt32Clamped(1); got != 1 {
		t.Fatalf("positive int64 = %d, want 1", got)
	}
	if got := nonNegativeInt64ToInt32Clamped(int64(math.MaxInt32) + 1); got != math.MaxInt32 {
		t.Fatalf("large int64 = %d, want max", got)
	}
}
