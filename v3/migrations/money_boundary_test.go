package migrations

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Business code may allocate an empty account, but only the ledger and offline
// importer can change PostgreSQL balances. This protects the Poster boundary
// even while SQL remains handwritten pgx statements.
func TestPostgresMoneyWritesStayInsideLedger(t *testing.T) {
	update := regexp.MustCompile(`(?is)\bUPDATE\s+(?:"?v3_billing"?\.)?"?accounts"?\b[^;]*\bSET\b[^;]*\bbalance\s*=`)
	insert := regexp.MustCompile(`(?is)\bINSERT\s+INTO\s+(?:"?v3_billing"?\.)?"?accounts"?\s*\([^)]*\bbalance\b`)
	for _, root := range []string{"../internal", "../cmd"} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			name := filepath.ToSlash(path)
			if strings.HasPrefix(name, "../internal/billing/ledger/") || strings.HasPrefix(name, "../internal/legacy/") {
				return nil
			}
			positions := token.NewFileSet()
			file, err := parser.ParseFile(positions, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(node ast.Node) bool {
				literal, ok := node.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				text, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Error(err)
					return true
				}
				if update.MatchString(text) || insert.MatchString(text) {
					t.Errorf("%s: balance write must use billing.Poster", positions.Position(literal.Pos()))
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
