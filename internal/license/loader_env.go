package license

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/viper"
)

// LoaderFromEnv gives the key that EE_LICENSE and EE_LICENSE_FILE name. The file is read
// on each call, so a renewed key is picked up by the next refresh; when both are set, the
// file wins. With neither set the loader has nothing to give.
//
// Temporary: it keeps the API and the worker reading their key as they always did while
// the Provider changes under them. The API stops using it in the next step and the worker
// in the one after, which deletes it.
func LoaderFromEnv() Loader {
	value := viper.GetString("EE_LICENSE")
	file := viper.GetString("EE_LICENSE_FILE")
	if file == "" {
		return func(context.Context) (string, error) { return value, nil }
	}
	if value != "" {
		slog.Default().Warn("both EE_LICENSE and EE_LICENSE_FILE are set: the file wins")
	}
	return func(context.Context) (string, error) {
		raw, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("the license file cannot be read: %w", unwrapPathError(err))
		}
		// An empty file is a fault, not the absence of a license: answering nothing would
		// hide it from the operator who wrote the path.
		if len(bytes.TrimSpace(raw)) == 0 {
			return "", errors.New("the license file is empty")
		}
		return string(raw), nil
	}
}

// unwrapPathError drops the path from a file error, keeping the operation's cause.
func unwrapPathError(err error) error {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err
	}
	return err
}
