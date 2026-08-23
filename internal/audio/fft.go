package audio

import "math"

/*
nextPow2 returns the smallest power of two >= n.

Called by:
  - yinDiffFFT when sizing the zero-padded FFT buffer

Output:
  - int: smallest power of two that is >= n
*/
func nextPow2(n int) int {
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

/*
fft performs an in-place iterative radix-2 Cooley-Tukey FFT.

Input:
  - a: []complex128 - buffer to transform, len(a) MUST be a power of two
  - invert: bool - false for forward transform, true for inverse

Called by:
  - yinDiffFFT to compute the linear autocorrelation via the
    Wiener-Khinchin theorem (FFT -> power spectrum -> inverse FFT)

Logic:
 1. Bit-reversal permutation of the input
 2. Iterative butterfly passes doubling the transform length each round
 3. If inverting, scale every element by 1/n

Output:
  - None (transforms a in place)
*/
func fft(a []complex128, invert bool) {
	n := len(a)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			a[i], a[j] = a[j], a[i]
		}
	}

	for length := 2; length <= n; length <<= 1 {
		ang := -2 * math.Pi / float64(length)
		if invert {
			ang = -ang
		}
		wlen := complex(math.Cos(ang), math.Sin(ang))
		for i := 0; i < n; i += length {
			w := complex(1.0, 0.0)
			half := length / 2
			for j := 0; j < half; j++ {
				u := a[i+j]
				v := a[i+j+half] * w
				a[i+j] = u + v
				a[i+j+half] = u - v
				w *= wlen
			}
		}
	}

	if invert {
		nc := complex(float64(n), 0)
		for i := range a {
			a[i] /= nc
		}
	}
}
