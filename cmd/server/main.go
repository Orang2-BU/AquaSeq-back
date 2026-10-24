package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"aquaseq-back/internal/ai"
	"aquaseq-back/internal/auth"
	"aquaseq-back/internal/handler"
	"aquaseq-back/internal/learning"
	"aquaseq-back/internal/repository"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "18080"
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("FATAL: Variabel lingkungan JWT_SECRET wajib diatur (minimal 16 karakter).")
	}

	jwtAuth, err := auth.New(jwtSecret)
	if err != nil {
		log.Fatalf("FATAL: Inisialisasi JWT gagal: %v", err)
	}

	dbURL := os.Getenv("DATABASE_URL")
	var store repository.WebStore

	if dbURL != "" {
		maxConns := 20
		if mc := os.Getenv("DATABASE_MAX_CONNS"); mc != "" {
			if n, err := strconv.Atoi(mc); err == nil && n > 0 {
				maxConns = n
			}
		}
		pgStore, err := repository.OpenPostgres(context.Background(), dbURL, maxConns)
		if err != nil {
			log.Fatalf("FATAL: Gagal menghubungkan ke PostgreSQL: %v", err)
		}
		defer pgStore.Close()
		store = pgStore
		log.Printf("Penyimpanan: PostgreSQL aktif (pool: %d)", maxConns)
	} else {
		store = repository.NewMemoryStore()
		log.Println("Penyimpanan: In-Memory aktif (DATABASE_URL tidak disetel)")
	}

	demoEnabled := os.Getenv("DEMO_ENABLED") != "false"
	webURL := os.Getenv("WEB_URL")
	if webURL == "" {
		webURL = "http://localhost:3000"
	}

	filesDir := os.Getenv("FILES_DIR")
	if filesDir == "" {
		filesDir = "./uploads"
	}

	caseBase := &learning.CaseBase{}

	var liveHandler *handler.LiveHandler
	cfg := handler.Config{
		Auth:        jwtAuth,
		DemoEnabled: demoEnabled,
		FilesDir:    filesDir,
		WebURL:      webURL,
		SMTPHost:    os.Getenv("SMTP_HOST"),
		SMTPPort:    os.Getenv("SMTP_PORT"),
		SMTPUser:    os.Getenv("SMTP_USER"),
		SMTPPass:    os.Getenv("SMTP_PASS"),
		SMTPFrom:    os.Getenv("SMTP_FROM"),
		RegisterExtra: func(mux *http.ServeMux, withAuth func(http.HandlerFunc) http.HandlerFunc) {
			authFunc := liveHandler.Authenticate

			aiDeps := handler.AIDependencies{
				Auth:          authFunc,
				AuthorizePond: liveHandler.AuthorizePond,
				Decider:       ai.NewClientFromEnv(),
				PondData:      handler.NewStorePondProvider(store),
				CaseBase:      caseBase,
			}
			handler.RegisterAI(mux, aiDeps)
		},
	}

	liveHandler = handler.NewLiveHandler(store, cfg)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      liveHandler.Routes(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("AquaSeq Server aktif di port %s", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server startup gagal: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Menghentikan server AquaSeq secara aman...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Paksa penghentian: %v", err)
	}

	log.Println("Server AquaSeq berhasil dihentikan.")
}
