// Command spotis is a self-hosted virtual-app layer in front of one real Spotify app.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"spotis/internal/config"
	"spotis/internal/crypto"
	"spotis/internal/server"
	"spotis/internal/store"
)

func main() {
	cfg := config.Load()
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		log.Fatal(err)
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "spotis.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	box, err := crypto.New(cfg.SecretKey, cfg.DataDir)
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		for {
			st.Cleanup(cfg.RetentionDays)
			time.Sleep(time.Hour)
		}
	}()

	srv := &http.Server{Addr: cfg.Addr, Handler: server.New(cfg, st, box), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		sc, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		_ = srv.Shutdown(sc)
	}()
	log.Printf("spotis listening on %s (public URL %s)", cfg.Addr, cfg.PublicURL)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
