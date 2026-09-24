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
// reason given. Only the package itself: a directory below a reader is held to allowedImports
// like any other. Each reader narrows its clients to interfaces whose method sets, and whose
// place among the reader's fields, its own test pins.
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
		"already read; held to this list too, below",
}

// readerImports is what a reader may import on top of allowedImports: what it takes to hold a
// Connect client, and what its consent takes.
var readerImports = map[string]string{
	"connectrpc.com/connect": "requests and responses of the clients the reader pins",
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect": "the " +
		"clients, narrowed by the reader to pinned interfaces",
	"crypto/rand": "the id of a question put to the person",
	"sync":        "the consents a reader keeps",
}

// sources other than Go are refused outright: assembly or C in this tree could reach anything.
var foreignSources = []string{".s", ".S", ".c", ".cc", ".cpp", ".h", ".m", ".syso"}

func Test_Imports_OnlyTheReadersReachTheApi(t *testing.T) {
	t.Parallel()

	sawReaders := map[string]bool{}
	checked := checkImports(t, ".", func(dir string) map[string]string {
		if _, ok := readers[dir]; ok {
			sawReaders[dir] = true
			return withReaderImports()
		}
		return allowedImports
	})

	// A test that walked nothing would pass forever.
	for reader := range readers {
		require.True(t, sawReaders[reader], "%s is gone: its exemption no longer names anything", reader)
	}
	require.NotZero(t, checked, "no file of the MCP surface was checked")
}

// The packages allowedImports lets in whole are held to the same list, or a file added there
// later would carry a client into the MCP surface unseen.
func Test_Imports_AllowedPackagesStayPure(t *testing.T) {
	t.Parallel()
	checked := checkImports(t, "../connection", func(string) map[string]string { return allowedImports })
	require.NotZero(t, checked)
}

func withReaderImports() map[string]string {
	allowed := maps.Clone(allowedImports)
	maps.Copy(allowed, readerImports)
	return allowed
}

// checkImports holds every non-test Go file under root to the imports allowedFor its directory,
// and returns how many files it checked.
func checkImports(t *testing.T, root string, allowedFor func(dir string) map[string]string) int {
	t.Helper()
	fset := token.NewFileSet()
	checked := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		require.Falsef(t, slices.Contains(foreignSources, filepath.Ext(path)),
			"%s is not Go: the import rule cannot see what it reaches", path)
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
		allowed := allowedFor(filepath.Dir(path))
		for _, spec := range file.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			// A package of this tree is walked in its turn, and held to its own list.
			if strings.HasPrefix(imported, mcpTree) {
				continue
			}
			_, ok := allowed[imported]
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
	return checked
}
