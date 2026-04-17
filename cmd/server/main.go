package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"newsanalisator/internal/auth"
	"newsanalisator/internal/config"
	"newsanalisator/internal/db"
	"newsanalisator/internal/handlers"
	"newsanalisator/internal/images"
	"newsanalisator/internal/processor"
	"newsanalisator/internal/repo"
	"newsanalisator/internal/scheduler"
)

func main() {
	cfg := config.Load()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	database, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	r := repo.New(database.Pool)

	if n, _ := r.CountUsers(ctx); n == 0 && cfg.InitAdminEmail != "" && cfg.InitAdminPassword != "" {
		hash, err := auth.HashPassword(cfg.InitAdminPassword)
		if err != nil {
			log.Fatalf("hash: %v", err)
		}
		if _, err := r.CreateUser(ctx, cfg.InitAdminEmail, hash); err != nil {
			log.Printf("create admin: %v", err)
		} else {
			log.Printf("created admin user %s", cfg.InitAdminEmail)
		}
	}

	imagesDir := filepath.Join(cfg.StorageDir, "images")
	_ = os.MkdirAll(imagesDir, 0o755)
	img := images.New(imagesDir, cfg.PublicBaseURL)

	a := auth.New(r, cfg.JWTSecret, cfg.AccessTTLMin, cfg.RefreshTTLHours)
	p := processor.New(r, img)
	sch := scheduler.New(r, p)
	sch.Start(ctx)

	h := handlers.New(r, a, sch, p, imagesDir)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           h.Mux(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("listening on %s", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Printf("shutting down")
	shCtx, shCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shCancel()
	_ = srv.Shutdown(shCtx)
	cancel()
}
