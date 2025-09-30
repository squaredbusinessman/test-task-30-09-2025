package downloader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestRestoreFromStorageResetsTransientStates(t *testing.T) {
	tmp := t.TempDir()
	st, err := storage.NewFileStorage(filepath.Join(tmp, "tasks.json"))
	if err != nil {
		t.Fatalf("storage init: %v", err)
	}

	task := &storage.Task{
		ID:     "task",
		Status: "partial",
		Parts: []storage.FilePart{
			{URL: "https://example.com/a", FileName: "a.bin", Status: "done"},
			{URL: "https://example.com/b", FileName: "b.bin", Status: "downloading"},
		},
	}
	st.Put(task)

	mgr := NewManager(st, tmp, 0)
	if err := mgr.RestoreFromStorage(); err != nil {
		t.Fatalf("restore: %v", err)
	}

	got, ok := st.Get(task.ID)
	if !ok {
		t.Fatalf("expected task in storage")
	}
	if got.Status != "running" {
		t.Fatalf("expected status to reset to running, got %q", got.Status)
	}
	if got.Parts[1].Status != "pending" {
		t.Fatalf("expected part to reset to pending, got %q", got.Parts[1].Status)
	}

	select {
	case enqueued := <-mgr.jobCh:
		if enqueued.ID != task.ID {
			t.Fatalf("unexpected task enqueued: %q", enqueued.ID)
		}
	default:
		t.Fatalf("expected task to be enqueued for processing")
	}

	mgr.Shutdown()
}

func TestManagerCreateTaskPersistsAndEnqueues(t *testing.T) {
	tmp := t.TempDir()
	st, err := storage.NewFileStorage(filepath.Join(tmp, "tasks.json"))
	if err != nil {
		t.Fatalf("storage init: %v", err)
	}

	mgr := NewManager(st, tmp, 0)

	urls := []string{
		"https://example.com/files/data.bin",
		"https://example.com/files/data.bin?version=2",
	}

	task, err := mgr.CreateTask(context.Background(), urls)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if task.Status != "running" {
		t.Fatalf("expected status running, got %q", task.Status)
	}
	if len(task.Parts) != len(urls) {
		t.Fatalf("expected %d parts, got %d", len(urls), len(task.Parts))
	}

	seen := make(map[string]struct{})
	for _, part := range task.Parts {
		if part.Status != "pending" {
			t.Fatalf("expected part pending, got %q", part.Status)
		}
		if _, dup := seen[part.FileName]; dup {
			t.Fatalf("expected unique file names, got duplicate %q", part.FileName)
		}
		seen[part.FileName] = struct{}{}
	}

	stored, ok := st.Get(task.ID)
	if !ok {
		t.Fatalf("task not persisted to storage")
	}
	if stored.Status != "running" {
		t.Fatalf("expected persisted status running, got %q", stored.Status)
	}

	select {
	case enqueued := <-mgr.jobCh:
		if enqueued.ID != task.ID {
			t.Fatalf("unexpected task enqueued: %q", enqueued.ID)
		}
	default:
		t.Fatalf("expected task to be enqueued")
	}

	mgr.Shutdown()
}

func TestManagerDownloadsFromHTTPServer(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	stateDir := filepath.Join(tmp, "state")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}

	st, err := storage.NewFileStorage(filepath.Join(stateDir, "tasks.json"))
	if err != nil {
		t.Fatalf("storage init: %v", err)
	}

	payload := []byte(strings.Repeat("hello", 50))
	var mu sync.Mutex
	var rangeHeaders []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		rangeHeaders = append(rangeHeaders, r.Header.Get("Range"))
		mu.Unlock()

		start := 0
		if rng := r.Header.Get("Range"); rng != "" {
			if strings.HasPrefix(rng, "bytes=") {
				if n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(rng, "bytes="), "-")); err == nil {
					start = n
				}
			}
		}
		if start > len(payload) {
			start = len(payload)
		}

		status := http.StatusOK
		if start > 0 {
			status = http.StatusPartialContent
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)-start))
		w.WriteHeader(status)
		_, _ = w.Write(payload[start:])
	}))
	defer srv.Close()

	mgr := NewManager(st, dataDir, 1)
	if err := mgr.RestoreFromStorage(); err != nil {
		t.Fatalf("restore: %v", err)
	}

	task, err := mgr.CreateTask(context.Background(), []string{srv.URL + "/file.bin"})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		stored, ok := st.Get(task.ID)
		if ok && stored.Status == "done" {
			if len(stored.Parts) != 1 {
				t.Fatalf("expected single part, got %d", len(stored.Parts))
			}
			filePath := filepath.Join(dataDir, stored.Parts[0].FileName)
			data, err := os.ReadFile(filePath)
			if err != nil {
				t.Fatalf("read downloaded file: %v", err)
			}
			if string(data) != string(payload) {
				t.Fatalf("unexpected file content: got %d bytes", len(data))
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task did not finish in time")
		}
		time.Sleep(20 * time.Millisecond)
	}

	mgr.Shutdown()

	mu.Lock()
	defer mu.Unlock()
	if len(rangeHeaders) == 0 {
		t.Fatalf("expected at least one request to test server")
	}
}
