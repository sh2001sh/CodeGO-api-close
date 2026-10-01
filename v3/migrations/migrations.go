// Package migrations embeds the versioned v3 schema. Production applies it
// with Atlas (cmd/migrate); tests apply it in file-name order.
package migrations

import (
	"embed"
	"io/fs"
	"sort"
)

//go:embed *.sql
var files embed.FS

// Files returns migration file names in apply order.
func Files() ([]string, error) {
	names, err := fs.Glob(files, "*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}

// Read returns the SQL of one migration file.
func Read(name string) (string, error) {
	b, err := files.ReadFile(name)
	return string(b), err
}
