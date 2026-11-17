// Command go-classic-mpa-sample runs the issue tracker web app.
//
// It is a classic multi-page application: server-rendered HTML, plain HTML
// forms for every state change, and no JavaScript. It binds to localhost by
// default because there is no authentication — do not expose it publicly.
//
// The "init" subcommand (or the -init flag) prepares the database file and
// schema, refreshes planner statistics, prints a report, and exits without
// starting the server.
//
// The gh-like CLI is a separate binary, cmd/issues — clients of the web
// API should not have the server embedded in their tool.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/events"
	"github.com/podhmo/go-classic-mpa-sample/internal/store"
	"github.com/podhmo/go-classic-mpa-sample/internal/web"
)

func main() {
	addr := flag.String("addr", envOr("ISSUES_ADDR", "127.0.0.1:8080"),
		"listen address (env ISSUES_ADDR)")
	dbPath := flag.String("db", envOr("ISSUES_DB", "issues.db"),
		"SQLite database file path (env ISSUES_DB)")
	initFlag := flag.Bool("init", false,
		"initialize the database (create file, apply schema, ANALYZE) and exit; "+
			"equivalent to the 'init' subcommand")
	apiToken := flag.String("api-token", envOr("ISSUES_API_TOKEN", ""),
		"Bearer token for the JSON API under /api/v1 (env ISSUES_API_TOKEN); "+
			"empty disables the API entirely")
	flag.Parse()

	// An explicitly empty -db would open an unnamed ephemeral SQLite
	// database whose data silently vanishes; fail fast instead.
	if *dbPath == "" {
		log.Fatal("empty database path: pass -db PATH or set ISSUES_DB")
	}

	// The "init" subcommand or -init flag selects init mode: prepare the
	// database and exit instead of serving. flag stops parsing at the
	// first positional argument, so flags must precede "init" and
	// flag.Arg(0) holds it. -addr is irrelevant in this mode and ignored.
	if *initFlag || flag.Arg(0) == "init" {
		initDB(*dbPath)
		return
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open database %q: %v", *dbPath, err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			log.Printf("close database: %v", err)
		}
	}()

	// A database created before workspaces existed needs its default one
	// before seeds or scoped stores touch it.
	if err := st.EnsureDefaultWorkspace(context.Background()); err != nil {
		log.Fatalf("ensure default workspace: %v", err)
	}

	// SeedDemoData is a no-op when the issues table is not empty, so it is
	// safe to call on every startup.
	if err := st.SeedDemoData(context.Background()); err != nil {
		log.Fatalf("seed demo data: %v", err)
	}

	// /api/v1 mounts when either credential exists: the shared env token,
	// or at least one per-user api_tokens row. With neither it stays
	// fail-closed. A token issued while the server runs does not remount
	// it — restarting re-evaluates.
	hasAPITokens, err := st.HasAPITokens(context.Background())
	if err != nil {
		log.Fatalf("check api tokens: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Live updates: every mutator records its hints in the changes table
	// inside the mutation's own tx, and WatchChanges re-publishes new rows
	// to the hub — so writes from other processes (the local CLI) reach
	// the SSE endpoints too (docs/sketch/ja/live-updates.md, 案(a)). The
	// watermark is sampled synchronously so a commit cannot slip between
	// goroutine start and its first read.
	hub := events.NewHub()
	watermark, err := st.LatestChangeSeq(ctx)
	if err != nil {
		log.Fatalf("read changes watermark: %v", err)
	}
	go st.WatchChanges(ctx, watermark, hub.Publish)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           web.New(st, web.WithAPIToken(*apiToken), web.WithAPIEnabled(*apiToken != "" || hasAPITokens), web.WithHub(hub)),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		// WriteTimeout covers SSE too, but each events handler lifts its
		// own write deadline via ResponseController — a global 15s cap is
		// still right for ordinary responses.
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

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

// initDB implements the "init" subcommand / -init flag: create the database
// file if missing, ensure the full schema (all tables and their indexes),
// refresh query-planner statistics, print a report, and return. Everything
// is idempotent, so init is deterministic and safe to re-run.
func initDB(path string) {
	// Stat before Open (which creates the file) to report whether the
	// database is new. A stat failure other than "not exist" is reported
	// as "created" — Open will surface the real error if the path is
	// unusable.
	state := "created"
	if _, err := os.Stat(path); err == nil {
		state = "already existed"
	}

	// log.Fatalf exits without running deferred calls, so the store is
	// closed explicitly on every path.
	st, err := store.Open(path)
	if err != nil {
		log.Fatalf("open database %q: %v", path, err)
	}

	// Demo data stays a startup concern, but the fixed discussion
	// categories are reference data with no other source — without them a
	// schema-only database can never accept a discussion via the local CLI.
	ctx := context.Background()
	// init always guarantees the one mandatory workspace.
	if err := st.EnsureDefaultWorkspace(ctx); err != nil {
		st.Close()
		log.Fatalf("ensure default workspace in %q: %v", path, err)
	}
	if err := st.SeedCategories(ctx); err != nil {
		st.Close()
		log.Fatalf("seed categories in %q: %v", path, err)
	}
	if err := st.Analyze(ctx); err != nil {
		st.Close()
		log.Fatalf("analyze database %q: %v", path, err)
	}
	tables, indexes, err := st.SchemaObjects(ctx)
	if err != nil {
		st.Close()
		log.Fatalf("inspect schema of %q: %v", path, err)
	}
	if err := st.Close(); err != nil {
		log.Fatalf("close database %q: %v", path, err)
	}

	fmt.Printf("initialized database at %s (%s)\n", path, state)
	fmt.Printf("tables:   %s\n", strings.Join(tables, ", "))
	fmt.Printf("indexes:  %s\n", strings.Join(indexes, ", "))
	fmt.Println("analyze:  done")
}
