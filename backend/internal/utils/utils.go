package utils

import (
	"math"
	"net/http"
	"strings"

	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
)

func FilterSlice[T any](slice []T, filterFn func(T) bool) []T {
	filteredResults := []T{}
	for _, element := range slice {
		if filterFn(element) {
			filteredResults = append(filteredResults, element)
		}
	}
	return filteredResults
}

func MapSlice[T any, V any](slice []T, fn func(T) V) []V {
	newSlice := make([]V, len(slice))
	for index, element := range slice {
		newSlice[index] = fn(element)
	}
	return newSlice
}

func AllElementsEqual[T comparable](slice []T, value T) bool {
	for _, el := range slice {
		if el != value {
			return false
		}
	}
	return true
}

// ClampUint32 brings a count within the bounds of a uint32: a negative count is 0 and a
// count above the type's maximum is that maximum.
func ClampUint32(n int) uint32 {
	if n < 0 {
		return 0
	}
	if int64(n) > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(n)
}

func AnyElementEqual[T comparable](slice []T, value T) bool {
	for _, el := range slice {
		if el == value {
			return true
		}
	}
	return false
}

func NoElementEqual[T comparable](slice []T, value T) bool {
	for _, el := range slice {
		if el == value {
			return false
		}
	}
	return true
}

func GetBearerTokenFromHeader(
	header http.Header,
	key string,
) (string, error) {
	unparsedToken := header.Get(key)
	if unparsedToken == "" {
		return "", husonymerrors.NewUnauthenticated("must provide valid bearer token")
	}
	pieces := strings.Split(unparsedToken, " ")
	if len(pieces) != 2 {
		return "", husonymerrors.NewUnauthenticated("token not in proper format")
	}
	if pieces[0] != "Bearer" {
		return "", husonymerrors.NewUnauthenticated("must provided bearer token")
	}
	token := pieces[1]
	return token, nil
}
