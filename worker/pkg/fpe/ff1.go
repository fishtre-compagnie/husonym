// Package fpe implements FF1 (NIST SP 800-38G), a format-preserving encryption: a
// string of n decimal digits is encrypted into another string of n decimal digits.
//
// For anonymization, what matters is that FF1 is a keyed permutation of the strings
// of a given length: two distinct inputs never give the same output, the same input
// always gives the same output under the same key and tweak, and without the key
// the output says nothing of the input. Only encryption is implemented: nothing in
// Husonym reverses a pseudonym.
package fpe

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
)

const (
	radix  = 10
	rounds = 10
	// MinLength is the shortest input FF1 takes: the Feistel network needs two halves.
	MinLength = 2
	// maxLength bounds the input and the tweak, whose lengths FF1 encodes on 32 bits.
	maxLength = math.MaxUint32
)

// FF1 encrypts strings of decimal digits under one AES key.
type FF1 struct {
	block cipher.Block
}

// NewFF1 builds an FF1 cipher from an AES key of 16, 24 or 32 bytes.
func NewFF1(key []byte) (*FF1, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("fpe: %w", err)
	}
	return &FF1{block: block}, nil
}

// Encrypt returns the digits of digits encrypted under the tweak. digits holds the
// values 0 to 9, one per position, and the result has the same length.
func (f *FF1) Encrypt(digits, tweak []byte) ([]byte, error) {
	n, t := len(digits), len(tweak)
	if n < MinLength {
		return nil, fmt.Errorf("fpe: FF1 takes at least %d digits, got %d", MinLength, n)
	}
	if n > maxLength || t > maxLength {
		return nil, fmt.Errorf("fpe: FF1 takes at most %d digits and tweak bytes", maxLength)
	}
	for _, d := range digits {
		if d >= radix {
			return nil, errors.New("fpe: FF1 takes decimal digits only")
		}
	}

	u := n / 2
	v := n - u
	a := append([]byte(nil), digits[:u]...)
	b := append([]byte(nil), digits[u:]...)

	// Bytes needed to hold a number of v digits, and bytes of PRF output kept per round.
	byteLen := (bitLen(v) + 7) / 8
	d := 4*((byteLen+3)/4) + 4

	p := make([]byte, 16)
	p[0], p[1], p[2] = 1, 2, 1
	p[3], p[4], p[5] = byte(radix>>16), byte(radix>>8), byte(radix)
	p[6] = 10
	p[7] = byte(u % 256)
	binary.BigEndian.PutUint32(p[8:12], uint32(n))
	binary.BigEndian.PutUint32(p[12:16], uint32(t))

	pad := mod(-t-byteLen-1, 16)
	q := make([]byte, t+pad+1+byteLen)
	copy(q, tweak)

	radixPowU := new(big.Int).Exp(big.NewInt(radix), big.NewInt(int64(u)), nil)
	radixPowV := new(big.Int).Exp(big.NewInt(radix), big.NewInt(int64(v)), nil)

	for i := range rounds {
		q[t+pad] = byte(i)
		numB := num(b)
		clear(q[t+pad+1:])
		numB.FillBytes(q[t+pad+1:])

		r := f.prf(append(append([]byte(nil), p...), q...))
		s := f.expand(r, d)
		y := new(big.Int).SetBytes(s)

		m, radixPowM := u, radixPowU
		if i%2 == 1 {
			m, radixPowM = v, radixPowV
		}
		c := new(big.Int).Add(num(a), y)
		c.Mod(c, radixPowM)

		a, b = b, str(c, m)
	}
	return append(a, b...), nil
}

// prf is the CBC-MAC of x under the key, x being a multiple of 16 bytes long.
func (f *FF1) prf(x []byte) []byte {
	y := make([]byte, 16)
	for off := 0; off < len(x); off += 16 {
		for j := range 16 {
			y[j] ^= x[off+j]
		}
		f.block.Encrypt(y, y)
	}
	return y
}

// expand stretches the 16-byte block r to d bytes: r, then CIPH(r ⊕ [j]¹⁶) for j = 1, 2…
func (f *FF1) expand(r []byte, d int) []byte {
	s := append(make([]byte, 0, d+16), r...)
	for j := 1; len(s) < d; j++ {
		block := make([]byte, 16)
		binary.BigEndian.PutUint64(block[8:], uint64(j))
		for k := range 16 {
			block[k] ^= r[k]
		}
		f.block.Encrypt(block, block)
		s = append(s, block...)
	}
	return s[:d]
}

// bitLen is ⌈v·log₂(10)⌉, the bits needed to hold a number of v decimal digits.
func bitLen(v int) int {
	largest := new(big.Int).Exp(big.NewInt(radix), big.NewInt(int64(v)), nil)
	return largest.Sub(largest, big.NewInt(1)).BitLen()
}

func num(digits []byte) *big.Int {
	x := new(big.Int)
	ten := big.NewInt(radix)
	for _, d := range digits {
		x.Mul(x, ten)
		x.Add(x, big.NewInt(int64(d)))
	}
	return x
}

// str writes x, lower than 10^m, as m decimal digits, most significant first.
func str(x *big.Int, m int) []byte {
	text := x.Text(radix)
	out := make([]byte, m)
	pad := m - len(text)
	for i := range len(text) {
		out[pad+i] = text[i] - '0'
	}
	return out
}

func mod(x, m int) int {
	return ((x % m) + m) % m
}
