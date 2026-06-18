package retrieve

import (
	"math"
	"testing"
)

func TestCosine(t *testing.T) {
	cases := []struct {
		name string
		a, b []float32
		want float64
	}{
		{"identical", []float32{1, 0}, []float32{1, 0}, 1},
		{"orthogonal", []float32{1, 0}, []float32{0, 1}, 0},
		{"opposite", []float32{1, 0}, []float32{-1, 0}, -1},
		{"scaled", []float32{1, 1}, []float32{2, 2}, 1},
		{"zero vector", []float32{0, 0}, []float32{1, 1}, 0},
		{"length mismatch", []float32{1, 0, 0}, []float32{1, 0}, 0},
		{"empty", []float32{}, []float32{}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cosine(tc.a, tc.b)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("cosine(%v,%v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
