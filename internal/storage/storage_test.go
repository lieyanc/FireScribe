package storage_test

import (
	"bytes"
	"testing"

	"github.com/lieyan/firescribe/internal/storage"
)

func TestStoreOriginalUsesContentHashPath(t *testing.T) {
	files, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	first, err := files.StoreOriginal("scan.png", bytes.NewBufferString("same-content"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := files.StoreOriginal("another.png", bytes.NewBufferString("same-content"))
	if err != nil {
		t.Fatal(err)
	}

	if first.SHA256 != second.SHA256 {
		t.Fatalf("hash mismatch: %s != %s", first.SHA256, second.SHA256)
	}
	if first.RelativePath != second.RelativePath {
		t.Fatalf("path mismatch: %s != %s", first.RelativePath, second.RelativePath)
	}
}
