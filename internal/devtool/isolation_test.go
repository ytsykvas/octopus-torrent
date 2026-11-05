package devtool

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestOnlyTheLibraryMayImportSQL(t *testing.T) {
	listing := "octopustorrent/internal/library context database/sql database/sql/driver modernc.org/sqlite modernc.org/sqlite/lib\n" +
		"octopustorrent/internal/app fmt octopustorrent/internal/library\n" +
		"octopustorrent/internal/torrentengine/filestore os database/sql\n" + // a test file's import
		"octopustorrent/internal/files zombiezen.com/go/sqlite\n" +
		"octopustorrent/internal/transfer database/sql/driver\n" +
		"octopustorrent/internal/torrentengine/filestore os database/sql\n" + // the same package in another build mode
		"octopustorrent/internal/sqlish strings\n"
	got := CheckSQLIsolation("octopustorrent", listing, []string{"react", "sql.js", "wa-sqlite", "vite"})
	want := []string{
		"frontend/package-lock.json: sql.js",
		"frontend/package-lock.json: wa-sqlite",
		"octopustorrent/internal/files imports zombiezen.com/go/sqlite",
		"octopustorrent/internal/torrentengine/filestore imports database/sql",
		"octopustorrent/internal/transfer imports database/sql/driver",
	}
	if !slices.Equal(got, want) {
		t.Errorf("CheckSQLIsolation =\n%q\nwant\n%q", got, want)
	}
	if got := CheckSQLIsolation("octopustorrent", "octopustorrent/internal/library database/sql\n", []string{"react"}); got != nil {
		t.Errorf("CheckSQLIsolation of a clean tree = %q", got)
	}
}

// The listing format names every import the go tool knows of a package: its files', its tests' and
// its external tests'. It runs the real go tool on a module of its own, so a format that drops one
// of them is caught.
func TestTheSQLListingIncludesTestImports(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")
	dir := t.TempDir()
	sys := OSSystem{}
	for name, text := range map[string]string{
		"go.mod":          "module example.com/m\n\ngo 1.27\n",
		"lib/lib.go":      "package lib\n",
		"lib/lib_test.go": "package lib\n\nimport _ \"database/sql\"\n",
		"lib/x_test.go":   "package lib_test\n\nimport _ \"database/sql/driver\"\n",
	} {
		if err := sys.MkdirAll(filepath.Join(dir, filepath.Dir(name))); err != nil {
			t.Fatal(err)
		}
		if err := sys.WriteFile(filepath.Join(dir, name), []byte(text)); err != nil {
			t.Fatal(err)
		}
	}
	out, err := sys.Capture("go", "-C", dir, "list", "-f", sqlImportsFormat, "./...")
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	want := []string{"example.com/m/lib imports database/sql", "example.com/m/lib imports database/sql/driver"}
	if got := CheckSQLIsolation("example.com/m", out, nil); !slices.Equal(got, want) {
		t.Errorf("CheckSQLIsolation of the real listing =\n%q\nwant\n%q\nlisting:\n%s", got, want, out)
	}
}
