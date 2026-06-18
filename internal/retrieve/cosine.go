package retrieve

import "math"

// cosine returns the cosine similarity of a and b in [-1, 1]. It returns 0 when
// the vectors are empty, have differing lengths, or either has zero magnitude.
// Vectors from one embedding model are always the same length, so a length
// mismatch indicates a programming error rather than a meaningful comparison.
func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
