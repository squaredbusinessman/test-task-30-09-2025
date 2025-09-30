package downloader

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"test-task-30-09-2025/internal/storage"
)

type Manager struct {
	storage     *storage.FileStorage
	downloadDir string
	workers     int

	mu      sync.Mutex
	wg      sync.WaitGroup
	closing bool
	jobCh   chan *storage.Task
}

func NewManager(st *storage.FileStorage, downloadDir string, workers int) *Manager {
	return &Manager{
		storage:     st,
		downloadDir: downloadDir,
		workers:     workers,
		jobCh:       make(chan *storage.Task, 256),
	}
}

func (m *Manager) RestoreFromStorage() error {
	// Enqueue tasks that are not done
	for _, t := range m.storage.List() {
		if t.Status == "done" {
			continue
		}
		// Reset transient states to pending
		for i := range t.Parts {
			if t.Parts[i].Status == "downloading" {
				t.Parts[i].Status = "pending"
			}
		}
		t.Status = "running"
		m.enqueue(t)
	}
	// Start workers
	for i := 0; i < m.workers; i++ {
		m.wg.Add(1)
		go m.worker()
	}
	return nil
}

func (m *Manager) Shutdown() {
	m.mu.Lock()
	m.closing = true
	close(m.jobCh)
	m.mu.Unlock()
	m.wg.Wait()
}

func (m *Manager) CreateTask(ctx context.Context, urls []string) (*storage.Task, error) {
	if len(urls) == 0 {
		return nil, errors.New("empty urls")
	}
	id := randomID()
	parts := make([]storage.FilePart, 0, len(urls))
	for _, u := range urls {
		parts = append(parts, storage.FilePart{
			URL:        u,
			FileName:   safeFileName(u),
			BytesTotal: 0,
			BytesDone:  0,
			Status:     "pending",
		})
	}
	task := &storage.Task{ID: id, CreatedAt: time.Now().Unix(), Status: "running", Parts: parts}
	m.storage.Put(task)
	m.enqueue(task)
	return task, nil
}

func (m *Manager) enqueue(task *storage.Task) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing {
		return
	}
	m.jobCh <- task
}

func (m *Manager) worker() {
	defer m.wg.Done()
	client := &http.Client{Timeout: 0}
	for task := range m.jobCh {
		m.processTask(client, task)
	}
}

func (m *Manager) processTask(client *http.Client, task *storage.Task) {
	allOK := true
	partial := false
	for i := range task.Parts {
		p := &task.Parts[i]
		if p.Status == "done" {
			continue
		}
		if err := m.downloadPart(client, p); err != nil {
			p.Status = "error"
			p.Error = err.Error()
			allOK = false
			partial = true
			m.storage.Put(task)
			continue
		}
		p.Status = "done"
		p.Error = ""
		m.storage.Put(task)
	}
	if allOK {
		task.Status = "done"
	} else if partial {
		task.Status = "partial"
	} else {
		task.Status = "error"
	}
	m.storage.Put(task)
}

func (m *Manager) downloadPart(client *http.Client, part *storage.FilePart) error {
	dstPath := filepath.Join(m.downloadDir, part.FileName)
	// Try resume
	var start int64 = 0
	if fi, err := os.Stat(dstPath); err == nil {
		start = fi.Size()
	}

	req, err := http.NewRequest(http.MethodGet, part.URL, nil)
	if err != nil {
		return err
	}
	if start > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", start))
	}
	part.Status = "downloading"

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}

	// Determine total size
	if resp.ContentLength > 0 {
		if start > 0 {
			part.BytesTotal = start + resp.ContentLength
		} else {
			part.BytesTotal = resp.ContentLength
		}
	}

	// Open file
	f, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if start > 0 {
		if _, err := f.Seek(start, 0); err != nil {
			return err
		}
		part.BytesDone = start
	}

	buf := make([]byte, 128*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			part.BytesDone += int64(n)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	// Sync to disk for durability
	if err := f.Sync(); err != nil {
		return err
	}
	return nil
}

func randomID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func safeFileName(u string) string {
	// naive: take last path segment, strip query
	s := u
	if idx := strings.Index(s, "?"); idx >= 0 {
		s = s[:idx]
	}
	if idx := strings.LastIndex(s, "/"); idx >= 0 {
		s = s[idx+1:]
	}
	if s == "" {
		s = randomID()
	}
	return s
}
