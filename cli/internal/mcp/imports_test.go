package mcp_server

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The MCP surface reads connections through maskedconn and through nothing else: secrets are
// write-only there (plans/mcp-husonym.md §6.1). A rule in prose would not hold — the reader in
// clear, the Connect client, is one import away — so this test holds it instead.
//
// It is an allowlist, not a denylist: a way of reading that does not exist yet is refused until
// someone adds it here, with the reason it cannot hand back a secret.

// maskedReaderDir is the one package under this tree allowed to hold a Connect client.
const maskedReaderDir = "maskedconn"

const mcpTree = "github.com/fishtre-compagnie/husonym/cli/internal/mcp/"

var allowedImports = map[string]string{
	"cmp":      "standard library, no I/O",
	"context":  "standard library, no I/O",
	"fmt":      "standard library, no I/O",
	"log/slog": "standard library, writes logs only",
	"slices":   "standard library, no I/O",
	"time":     "standard library, no I/O",

	"github.com/modelcontextprotocol/go-sdk/mcp": "the protocol itself",
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1": "message types only; " +
		"the clients live in mgmtv1alpha1connect, which is not allowed",
	"github.com/fishtre-compagnie/husonym/cli/internal/connection": "pure functions over a connection " +
		"already read",
}

func Test_Imports_OnlyTheMaskedReaderReachesTheApi(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	sawMaskedReader := false
	checked := 0
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path == maskedReaderDir {
				sawMaskedReader = true
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
					"Connections are read through %s only, so that no secret is ever read back; "+
					"if this import cannot hand one back, add it to allowedImports with the reason why.",
				path, imported, maskedReaderDir,
			)
		}
		return nil
	})
	require.NoError(t, err)

	// A test that walked nothing would pass forever.
	require.True(t, sawMaskedReader, "%s is gone: the exemption above no longer names anything", maskedReaderDir)
	require.NotZero(t, checked, "no file of the MCP surface was checked")
}
