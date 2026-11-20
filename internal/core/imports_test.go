package core_test

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// core holds the plan rules and knows nothing of storage, harnesses or
// screens (CLAUDE.md, issue #59 AC-1).
func TestCoreDoesNotImportStoreAgentOrTUI(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	const module = "github.com/admSla99/BeforeCode/internal/"
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			for _, banned := range []string{"store", "agent", "tui"} {
				if path == module+banned || strings.HasPrefix(path, module+banned+"/") {
					t.Errorf("%s imports %s", name, path)
				}
			}
		}
	}
}
