package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"test-task-30-09-2025/internal/api"
	"test-task-30-09-2025/internal/downloader"
	"test-task-30-09-2025/internal/storage"
)

func main() {
	// Configuration (could be extended via flags/env)
	dataDir := "data"
	stateDir := "state"
	addr := ":8080"
	workerCount := 4

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("failed to create data dir: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		log.Fatalf("failed to create state dir: %v", err)
	}

	// Storage
	st, err := storage.NewFileStorage(stateDir + "/tasks.json")
	if err != nil {
		log.Fatalf("failed to init storage: %v", err)
	}

	// Downloader
	mgr := downloader.NewManager(st, dataDir, workerCount)
	if err := mgr.RestoreFromStorage(); err != nil {
		log.Fatalf("failed to restore tasks: %v", err)
	}

	// API
	handler := api.NewHandler(st, mgr)
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler.Router(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Start server
	go func() {
		log.Printf("HTTP server listening on %s", addr)
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
