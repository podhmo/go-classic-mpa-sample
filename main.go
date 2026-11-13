// Command go-classic-mpa-sample runs the issue tracker web app.
//
// It is a classic multi-page application: server-rendered HTML, plain HTML
// forms for every state change, and no JavaScript. It binds to localhost by
// default because there is no authentication — do not expose it publicly.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
	"github.com/podhmo/go-classic-mpa-sample/internal/web"
)

func main() {
	addr := flag.String("addr", envOr("ISSUES_ADDR", "127.0.0.1:8080"),
		"listen address (env ISSUES_ADDR)")
	dbPath := flag.String("db", envOr("ISSUES_DB", "issues.db"),
		"SQLite database file path (env ISSUES_DB)")
	flag.Parse()

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open database %q: %v", *dbPath, err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			log.Printf("close database: %v", err)
		}
	}()

	// SeedDemoData is a no-op when the issues table is not empty, so it is
	// safe to call on every startup.
	if err := st.SeedDemoData(context.Background()); err != nil {
		log.Fatalf("seed demo data: %v", err)
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           web.New(st),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		log.Println("shutting down…")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown: %v", err)
		}
	}()

	log.Printf("issue tracker listening on http://%s (db: %s)", *addr, *dbPath)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve: %v", err)
	}
}

// envOr returns the environment variable value when set, otherwise def.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
