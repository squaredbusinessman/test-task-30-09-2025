package downloader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"test-task-30-09-2025/internal/storage"
)

func TestUniqueFileNameAddsSuffixOnCollision(t *testing.T) {
	tmp := t.TempDir()
	st, err := storage.NewFileStorage(filepath.Join(tmp, "tasks.json"))
	if err != nil {
		t.Fatalf("storage init: %v", err)
	}
	mgr := NewManager(st, tmp, 1)

	first := mgr.uniqueFileName("file.txt")
	if first != "file.txt" {
		t.Fatalf("expected base name to be kept, got %q", first)
	}

	second := mgr.uniqueFileName("file.txt")
	if second == first {
		t.Fatalf("expected unique name, got duplicate %q", second)
	}
	if !strings.HasPrefix(second, "file-") || !strings.HasSuffix(second, ".txt") {
		t.Fatalf("unexpected suffix format: %q", second)
	}
}

func TestUniqueFileNameAvoidsExistingFileOnDisk(t *testing.T) {
	tmp := t.TempDir()
	st, err := storage.NewFileStorage(filepath.Join(tmp, "tasks.json"))
	if err != nil {
		t.Fatalf("storage init: %v", err)
	}
	mgr := NewManager(st, tmp, 1)

	existing := filepath.Join(tmp, "asset.bin")
	if err := os.WriteFile(existing, []byte("test"), 0o644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	name := mgr.uniqueFileName("asset.bin")
	if name == "asset.bin" {
		t.Fatalf("expected name to change because file exists on disk")
	}
	if !strings.HasPrefix(name, "asset-") || !strings.HasSuffix(name, ".bin") {
		t.Fatalf("unexpected suffix format: %q", name)
	}
}
