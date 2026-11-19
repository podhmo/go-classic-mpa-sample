// Command zatudan is the server binary of the issue tracker web app — a
// classic multi-page application: server-rendered HTML, plain HTML forms
// for every state change, and progressive-enhancement JavaScript. It
// binds to localhost by default because there is no authorization — do
// not expose it publicly.
//
// The "init" subcommand (or the -init flag) prepares the database file
// and schema plus its master data (default workspace, labels, discussion
// categories), refreshes planner statistics, prints a report, and exits
// without starting the server. Demo issues/discussions are mock data,
// kept separate: they land only when -seed (env ZATUDAN_SEED) is set, in
// init mode as well as on server start.
//
//	zatudan -addr 127.0.0.1:8080 -db zatudan.db
//	zatudan -db zatudan.db -seed
//	zatudan -db zatudan.db init
//
// The gh-like CLI is a separate binary, cmd/zatu — clients of the web
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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/podhmo/go-classic-mpa-sample/internal/events"
	"github.com/podhmo/go-classic-mpa-sample/internal/gitrepo"
	"github.com/podhmo/go-classic-mpa-sample/internal/store"
	"github.com/podhmo/go-classic-mpa-sample/internal/web"
)

func main() {
	addr := flag.String("addr", envOr("ZATUDAN_ADDR", "127.0.0.1:8080"),
		"listen address (env ZATUDAN_ADDR)")
	dbPath := flag.String("db", envOr("ZATUDAN_DB", "zatudan.db"),
		"SQLite database file path (env ZATUDAN_DB)")
	initFlag := flag.Bool("init", false,
		"initialize the database (create file, apply schema, ANALYZE) and exit; "+
			"equivalent to the 'init' subcommand")
	apiToken := flag.String("api-token", envOr("ZATUDAN_API_TOKEN", ""),
		"Bearer token for the JSON API under /api/v1 (env ZATUDAN_API_TOKEN); "+
			"empty disables the API entirely")
	seed := flag.Bool("seed", envBool("ZATUDAN_SEED"),
		"insert demo issues, discussions and the demo user (env ZATUDAN_SEED); "+
			"master data (workspace, labels, categories) is ensured either way")
	repoPath := flag.String("repo", "",
		"git repository for pull requests (env ZATUDAN_REPO); "+
			"'off' disables the feature, empty auto-detects near -db")
	flag.Parse()

	// An explicitly empty -db would open an unnamed ephemeral SQLite
	// database whose data silently vanishes; fail fast instead.
	if *dbPath == "" {
		log.Fatal("empty database path: pass -db PATH or set ZATUDAN_DB")
	}

	// The "init" subcommand or -init flag selects init mode: prepare the
	// database and exit instead of serving. flag stops parsing at the
	// first positional argument, so flags must precede "init" and
	// flag.Arg(0) holds it. -addr is irrelevant in this mode and ignored.
	if *initFlag || flag.Arg(0) == "init" {
		initDB(*dbPath, *seed)
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

	// Master data (default workspace, discussion categories; labels come
	// with the schema) is ensured on every start so the app functions on a
	// fresh database. Demo data stays opt-in via -seed: without it the
	// server starts with master data only.
	if err := st.SeedMasterData(context.Background()); err != nil {
		log.Fatalf("ensure master data: %v", err)
	}
	if *seed {
		if err := st.SeedDemoData(context.Background()); err != nil {
			log.Fatalf("seed demo data: %v", err)
		}
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

	// Pull requests exist only with a git repo: explicit -repo failures
	// are startup errors, auto-detect failures silently leave the feature
	// unmounted (docs/sketch/ja/pull-requests.md §1).
	repo, err := gitrepo.Resolve(ctx, gitrepo.Options{
		RepoPath: *repoPath,
		EnvRepo:  os.Getenv("ZATUDAN_REPO"),
		DBPath:   *dbPath,
	})
	if err != nil {
		log.Fatalf("resolve repository: %v", err)
	}
	if repo != nil {
		log.Printf("git repository: %s (git %s)", repo.Dir, repo.Version)
	}

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
		Handler:           web.New(st, web.WithAPIToken(*apiToken), web.WithAPIEnabled(*apiToken != "" || hasAPITokens), web.WithHub(hub), web.WithRepo(repo)),
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

// envBool reports whether the environment variable holds a truthy value
// ("1", "true", ...); unset or unparseable means false.
func envBool(key string) bool {
	v, err := strconv.ParseBool(os.Getenv(key))
	return err == nil && v
}

// initDB implements the "init" subcommand / -init flag: create the database
// file if missing, ensure the full schema (all tables and their indexes)
// and the master data, refresh query-planner statistics, print a report,
// and return. With seed=true it also inserts the demo data. Everything is
// idempotent, so init is deterministic and safe to re-run.
func initDB(path string, seed bool) {
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

	// Master data has no other source: without the workspace and the
	// categories a schema-only database can never accept an issue or a
	// discussion via the local CLI. Demo data is mock data and stays
	// opt-in, matching the server's -seed flag.
	ctx := context.Background()
	if err := st.SeedMasterData(ctx); err != nil {
		st.Close()
		log.Fatalf("ensure master data in %q: %v", path, err)
	}
	if seed {
		if err := st.SeedDemoData(ctx); err != nil {
			st.Close()
			log.Fatalf("seed demo data in %q: %v", path, err)
		}
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
