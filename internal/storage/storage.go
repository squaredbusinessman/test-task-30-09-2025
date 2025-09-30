package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

type FilePart struct {
	URL        string `json:"url"`
	FileName   string `json:"file_name"`
	BytesTotal int64  `json:"bytes_total"`
	BytesDone  int64  `json:"bytes_done"`
	Status     string `json:"status"` // pending, downloading, done, error
	Error      string `json:"error,omitempty"`
}

type Task struct {
	ID        string     `json:"id"`
	CreatedAt int64      `json:"created_at"`
	Status    string     `json:"status"` // pending, running, done, error, partial
	Parts     []FilePart `json:"parts"`
}

type FileStorage struct {
	mu    sync.RWMutex
	path  string
	tasks map[string]*Task
	dirty bool
}

func NewFileStorage(path string) (*FileStorage, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	fs := &FileStorage{path: path, tasks: make(map[string]*Task)}
	if err := fs.load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return fs, nil
}

func (s *FileStorage) load() error {
	f, err := os.Open(s.path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	return dec.Decode(&s.tasks)
}

func (s *FileStorage) Flush() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.dirty {
		return nil
	}
	tmp := s.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s.tasks); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s *FileStorage) Save() error {
	s.mu.Lock()
	s.dirty = true
	s.mu.Unlock()
	return s.Flush()
}

func (s *FileStorage) Put(task *Task) {
	s.mu.Lock()
	s.tasks[task.ID] = task
	s.dirty = true
	s.mu.Unlock()
	_ = s.Flush()
}

func (s *FileStorage) Get(id string) (*Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	return t, ok
}

func (s *FileStorage) List() []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, t)
	}
	return out
}
