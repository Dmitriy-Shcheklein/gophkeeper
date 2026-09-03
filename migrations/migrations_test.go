package migrations

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
)

// wantFiles lists every migration the embedded FS must contain.
// The list grows with each new migration; keeping it explicit guards
// against accidental deletions or renames that would silently skip
// schema changes on deployed servers.
var wantFiles = []string{
	"000001_create_users.down.sql",
	"000001_create_users.up.sql",
	"000002_create_entries.down.sql",
	"000002_create_entries.up.sql",
}

func TestFSContainsExpectedMigrations(t *testing.T) {
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		t.Fatalf("read embedded FS: %v", err)
	}

	got := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			t.Errorf("unexpected directory in embedded FS: %s", e.Name())
			continue
		}
		got = append(got, e.Name())
	}
	slices.Sort(got)

	if !slices.Equal(got, wantFiles) {
		t.Fatalf("embedded FS files = %v, want %v", got, wantFiles)
	}
}

func TestEmbeddedSQLFilesAreNonEmpty(t *testing.T) {
	for _, name := range wantFiles {
		data, err := fs.ReadFile(FS, name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if len(strings.TrimSpace(string(data))) == 0 {
			t.Errorf("migration %s is empty", name)
		}
	}
}

func TestUpAndDownMigrationPairsMatch(t *testing.T) {
	for _, name := range wantFiles {
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		down := strings.TrimSuffix(name, ".up.sql") + ".down.sql"
		if !slices.Contains(wantFiles, down) {
			t.Errorf("migration %s has no down counterpart %s", name, down)
		}
	}
}
