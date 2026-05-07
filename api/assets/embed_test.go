package assets

import (
	"io/fs"
	"testing"
)

func TestLoadFileReturnsEmbeddedAssetSubtree(t *testing.T) {
	subtree := LoadFile()
	entries, err := fs.ReadDir(subtree, ".")
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("embedded assets subtree should not be empty")
	}
}
