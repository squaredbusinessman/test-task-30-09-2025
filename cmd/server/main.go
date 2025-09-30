package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"test-task-30-09-2025/internal/api"
	"test-task-30-09-2025/internal/downloader"
	"test-task-30-09-2025/internal/storage"
)

func main() {
	cfg := loadConfig()

	if cfg.workerCount <= 0 {
		log.Printf("invalid worker count %d, forcing to 1", cfg.workerCount)
		cfg.workerCount = 1
	}

	if err := os.MkdirAll(cfg.dataDir, 0o755); err != nil {
		log.Fatalf("failed to create data dir: %v", err)
	}
	if err := os.MkdirAll(cfg.stateDir, 0o755); err != nil {
		log.Fatalf("failed to create state dir: %v", err)
	}

	// Storage
	st, err := storage.NewFileStorage(cfg.stateDir + "/tasks.json")
	if err != nil {
		log.Fatalf("failed to init storage: %v", err)
	}

	// Downloader
	mgr := downloader.NewManager(st, cfg.dataDir, cfg.workerCount)
	if err := mgr.RestoreFromStorage(); err != nil {
		log.Fatalf("failed to restore tasks: %v", err)
	}

	// API
	handler := api.NewHandler(st, mgr)
	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           handler.Router(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Start server
	go func() {
		log.Printf("HTTP server listening on %s", cfg.addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// OS signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh
	log.Println("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop HTTP server first to stop new incoming requests
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown error: %v", err)
	}

	// Ask downloader to stop accepting new jobs and flush checkpoints
	mgr.Shutdown()

	// Final sync to storage
	if err := st.Flush(); err != nil {
		log.Printf("storage flush error: %v", err)
	}

	log.Println("shutdown complete")
}

type config struct {
	dataDir     string
	stateDir    string
	addr        string
	workerCount int
}

const (
	envDataDir     = "DOWNLOADER_DATA_DIR"
	envStateDir    = "DOWNLOADER_STATE_DIR"
	envAddr        = "DOWNLOADER_ADDR"
	envWorkerCount = "DOWNLOADER_WORKERS"
)

func loadConfig() config {
	cfg := config{
		dataDir:     envOrDefault(envDataDir, "data"),
		stateDir:    envOrDefault(envStateDir, "state"),
		addr:        envOrDefault(envAddr, ":8080"),
		workerCount: envOrInt(envWorkerCount, 4),
	}

	dataDirFlag := flag.String("data-dir", cfg.dataDir, "directory for downloaded files")
	stateDirFlag := flag.String("state-dir", cfg.stateDir, "directory for task state storage")
	addrFlag := flag.String("addr", cfg.addr, "HTTP listen address")
	workersFlag := flag.Int("workers", cfg.workerCount, "number of download workers")

	flag.Parse()

	cfg.dataDir = *dataDirFlag
	cfg.stateDir = *stateDirFlag
	cfg.addr = *addrFlag
	cfg.workerCount = *workersFlag

	return cfg
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil {
			log.Printf("invalid value for %s: %v", key, err)
			return fallback
		}
		return parsed
	}
	return fallback
}
