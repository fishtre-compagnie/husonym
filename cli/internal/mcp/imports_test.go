package mcp_server

import (
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The MCP surface reaches the API through its readers and through nothing else. maskedconn reads
// connections with their secrets masked, which keeps secrets write-only there
// (plans/mcp-husonym.md §6.1); novalues reads the structure of the data and never a row;
// rowvalues reads rows, and asks the person first. A rule in prose would not hold — a Connect
// client is one import away — so this test holds it instead.
//
// It is an allowlist, not a denylist: a way of reading that does not exist yet is refused until
// someone adds it here, with the reason it cannot hand back a secret.

// readers are the packages under this tree allowed to hold a Connect client, each for the
// reason given. Each one narrows its client to an interface whose method set its own test pins.
var readers = map[string]string{
	"maskedconn": "reads connections, and asks for every one with its secrets masked",
	"novalues":   "reads schemas and PII detections, never a value from a row",
	"rowvalues":  "reads values from rows, and only once the person has agreed for the connection",
}

const mcpTree = "github.com/fishtre-compagnie/husonym/cli/internal/mcp/"

var allowedImports = map[string]string{
	"cmp":           "standard library, no I/O",
	"context":       "standard library, no I/O",
	"encoding/json": "standard library, no I/O",
	"errors":        "standard library, no I/O",
	"fmt":           "standard library, no I/O",
	"log/slog":      "standard library, writes logs only",
	"maps":          "standard library, no I/O",
	"slices":        "standard library, no I/O",
	"strings":       "standard library, no I/O",
	"time":          "standard library, no I/O",

	"google.golang.org/protobuf/encoding/protojson": "encodes a message already read, no I/O",

	"github.com/modelcontextprotocol/go-sdk/mcp": "the protocol itself",
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1": "message types only; " +
		"the clients live in mgmtv1alpha1connect, which is not allowed",
	"github.com/fishtre-compagnie/husonym/cli/internal/connection": "pure functions over a connection " +
		"already read",
}

func Test_Imports_OnlyTheReadersReachTheApi(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	sawReaders := map[string]bool{}
	checked := 0
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if _, ok := readers[path]; ok {
				sawReaders[path] = true
				return filepath.SkipDir
			}
			return nil
		}
		// Tests are left out: they stand up a fake API, which takes the very client refused
		// here, and they do not ship.
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		checked++
		for _, spec := range file.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			// A package of this tree is walked in its turn, and held to the same list.
			if strings.HasPrefix(imported, mcpTree) {
				continue
			}
			_, ok := allowedImports[imported]
			require.Truef(t, ok,
				"%s imports %q, which is not on the allowlist of the MCP surface. "+
					"The API is reached through the readers only (%v), so that no secret and no row "+
					"is ever read back; if this import cannot hand one back, add it to allowedImports "+
					"with the reason why.",
				path, imported, slices.Sorted(maps.Keys(readers)),
			)
		}
		return nil
	})
	require.NoError(t, err)

	// A test that walked nothing would pass forever.
	for reader := range readers {
		require.True(t, sawReaders[reader], "%s is gone: its exemption no longer names anything", reader)
	}
	require.NotZero(t, checked, "no file of the MCP surface was checked")
}
