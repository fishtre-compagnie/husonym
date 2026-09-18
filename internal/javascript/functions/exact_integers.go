package javascript_functions

import (
	"math/big"
)

// maxExactInteger is the largest integer a JavaScript number holds exactly: 2^53.
const maxExactInteger = 1 << 53

// ToScript returns a value of a row as a script must receive it: every integer a
// JavaScript number would round — beyond ±2^53, such as a snowflake key — becomes a
// *big.Int, which the script receives as an exact BigInt. A script mixing it with numbers
// then fails, instead of writing a rounded key. Maps and slices holding none are returned
// as they are.
func ToScript(v any) any {
	if !holdsInexactInteger(v) {
		return v
	}
	return convert(v, func(leaf any) any {
		switch n := leaf.(type) {
		case int64:
			if n > maxExactInteger || n < -maxExactInteger {
				return big.NewInt(n)
			}
		case int:
			if n > maxExactInteger || n < -maxExactInteger {
				return big.NewInt(int64(n))
			}
		case uint64:
			if n > maxExactInteger {
				return new(big.Int).SetUint64(n)
			}
		}
		return leaf
	})
}

// FromScript returns a value a script left as the row holds it: a BigInt that fits an
// int64, or else a uint64, becomes one again.
func FromScript(v any) any {
	return convert(v, func(leaf any) any {
		if n, ok := leaf.(*big.Int); ok {
			switch {
			case n.IsInt64():
				return n.Int64()
			case n.IsUint64():
				return n.Uint64()
			}
		}
		return leaf
	})
}

func holdsInexactInteger(v any) bool {
	switch n := v.(type) {
	case int64:
		return n > maxExactInteger || n < -maxExactInteger
	case int:
		return n > maxExactInteger || n < -maxExactInteger
	case uint64:
		return n > maxExactInteger
	case map[string]any:
		for _, e := range n {
			if holdsInexactInteger(e) {
				return true
			}
		}
	case []any:
		for _, e := range n {
			if holdsInexactInteger(e) {
				return true
			}
		}
	}
	return false
}

// convert copies maps and slices, applying leaf to every other value.
func convert(v any, leaf func(any) any) any {
	switch n := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(n))
		for k, e := range n {
			out[k] = convert(e, leaf)
		}
		return out
	case []any:
		out := make([]any, len(n))
		for i, e := range n {
			out[i] = convert(e, leaf)
		}
		return out
	default:
		return leaf(v)
	}
}
