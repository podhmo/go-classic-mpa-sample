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
// When the first argument is a CLI subcommand (issue, comment, label,
// auth, api, ...) the binary behaves as the gh-like client instead —
// see internal/cli and docs/sketch/ja/cli-and-web-api.md.
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

	"github.com/podhmo/go-classic-mpa-sample/internal/cli"
	"github.com/podhmo/go-classic-mpa-sample/internal/store"
	"github.com/podhmo/go-classic-mpa-sample/internal/web"
)

func main() {
	if len(os.Args) > 1 && cli.IsSubcommand(os.Args[1]) {
		os.Exit(cli.Run(context.Background(), cli.Options{Args: os.Args[1:]}))
	}

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

	srv := &http.Server{
		Addr:              *addr,
		Handler:           web.New(st, web.WithAPIToken(*apiToken), web.WithAPIEnabled(*apiToken != "" || hasAPITokens)),
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

// initDB implements the "init" subcommand / -init flag: create the database
// file if missing, ensure the full schema (all tables and their indexes),
// refresh query-planner statistics, print a report, and return.
//
// It deliberately does not seed demo data — seeding stays a startup
// concern — so the operation is deterministic and safe to re-run: the
// schema DDL is idempotent and ANALYZE merely refreshes statistics.
// log.Fatalf exits without running deferred calls, so the store is closed
// explicitly on every path.
func initDB(path string) {
	// Stat before Open (which creates the file) to report whether the
	// database is new. A stat failure other than "not exist" is reported
	// as "created" — Open will surface the real error if the path is
	// unusable.
	state := "created"
	if _, err := os.Stat(path); err == nil {
		state = "already existed"
	}

	st, err := store.Open(path)
	if err != nil {
		log.Fatalf("open database %q: %v", path, err)
	}

	ctx := context.Background()
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
