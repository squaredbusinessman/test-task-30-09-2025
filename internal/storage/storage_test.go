package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileStoragePutAndReload(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "tasks.json")

	st, err := NewFileStorage(path)
	if err != nil {
		t.Fatalf("init storage: %v", err)
	}

	task := &Task{
		ID:        "task-1",
		CreatedAt: 123,
		Status:    "running",
		Parts: []FilePart{{
			URL:        "https://example.com/a.bin",
			FileName:   "a.bin",
			BytesTotal: 100,
			BytesDone:  20,
			Status:     "downloading",
		}},
	}

	st.Put(task)

	got, ok := st.Get(task.ID)
	if !ok {
		t.Fatalf("task not found via Get")
	}
	if got.ID != task.ID || len(got.Parts) != 1 {
		t.Fatalf("unexpected task retrieved: %+v", got)
	}

	// Ensure data persisted on disk by reloading
	st2, err := NewFileStorage(path)
	if err != nil {
		t.Fatalf("reload storage: %v", err)
	}
	got2, ok := st2.Get(task.ID)
	if !ok {
		t.Fatalf("task not found after reload")
	}
	if got2.Status != task.Status || got2.Parts[0].FileName != "a.bin" {
		t.Fatalf("unexpected task data after reload: %+v", got2)
	}
}

func TestFileStorageListReturnsAllTasks(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "tasks.json")

	st, err := NewFileStorage(path)
	if err != nil {
		t.Fatalf("init storage: %v", err)
	}

	for i := 0; i < 3; i++ {
		task := &Task{ID: fmt.Sprintf("task-%d", i)}
		st.Put(task)
	}

	list := st.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(list))
	}
}

func TestFileStorageSavePersistsManualChange(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "tasks.json")

	st, err := NewFileStorage(path)
	if err != nil {
		t.Fatalf("init storage: %v", err)
	}

	st.tasks["foo"] = &Task{ID: "foo", Status: "running"}
	if err := st.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read persisted file: %v", err)
	}
	if !strings.Contains(string(raw), "\"foo\"") {
		t.Fatalf("file does not contain saved task: %s", raw)
	}
}
