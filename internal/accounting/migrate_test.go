package accounting

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestListMigrationFilesSortsAndFiltersSQL(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		"0002_second.sql",
		"0001_first.sql",
		"README.txt",
		"0003_THIRD.SQL",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("select 1;"), 0o644); err != nil {
			t.Fatalf("write file %s: %v", name, err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := listMigrationFiles(dir)
	if err != nil {
		t.Fatalf("listMigrationFiles error: %v", err)
	}
	want := []string{"0001_first.sql", "0002_second.sql", "0003_THIRD.SQL"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected migration list, got=%v want=%v", got, want)
	}
}
