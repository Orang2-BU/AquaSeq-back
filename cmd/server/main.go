package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"aquaseq-back/internal/handler"
	"aquaseq-back/internal/repository"
	"aquaseq-back/internal/service"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// In-memory concurrent repository for fast standalone execution & demo mode
	// ConnectPostgres can be injected when DATABASE_URL is configured
	repo := repository.NewInMemRepository()
	svc := service.NewAppService(repo)
	h := handler.NewHandler(svc)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      h.RegisterRoutes(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("AquaSeq Engine running on port %s (HTTP REST API ready)", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server startup failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down AquaSeq Engine...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Forced shutdown error: %v", err)
	}

	log.Println("Server gracefully stopped.")
}
