package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"test-task-30-09-2025/internal/downloader"
	"test-task-30-09-2025/internal/storage"
)

type Handler struct {
	storage *storage.FileStorage
	manager *downloader.Manager
	mux     *http.ServeMux
}

func NewHandler(st *storage.FileStorage, mgr *downloader.Manager) *Handler {
	h := &Handler{storage: st, manager: mgr, mux: http.NewServeMux()}
	h.routes()
	return h
}

func (h *Handler) Router() http.Handler { return h.mux }

func (h *Handler) routes() {
	h.mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Create task
	h.mux.HandleFunc("/tasks", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			h.createTask(w, r)
		case http.MethodGet:
			h.listTasks(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	// Get task by id
	h.mux.HandleFunc("/tasks/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/tasks/")
		if id == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		h.getTask(w, r, id)
	})
}

type createTaskRequest struct {
	URLs []string `json:"urls"`
}

func (h *Handler) createTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if len(req.URLs) == 0 {
		http.Error(w, "urls required", http.StatusBadRequest)
		return
	}

	task, err := h.manager.CreateTask(r.Context(), req.URLs)
	if err != nil {
		log.Printf("create task error: %v", err)
		http.Error(w, "failed to create task", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(task)
}

func (h *Handler) getTask(w http.ResponseWriter, _ *http.Request, id string) {
	task, ok := h.storage.Get(id)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(task)
}

func (h *Handler) listTasks(w http.ResponseWriter, _ *http.Request) {
	tasks := h.storage.List()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tasks)
}
