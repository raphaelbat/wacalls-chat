package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	dbPath := flag.String("db", "wacalls.db", "SQLite session database path")
	staticDir := flag.String("static", "", "static client directory (optional; auto-detect 'dist' or 'client/dist')")
	debug := flag.Bool("debug", false, "verbose logging")
	maxCalls := flag.Int("max-calls-per-session", 8, "max concurrent calls per session (0 = unlimited)")
	seedAdminEmail := flag.String("seed-admin-email", "wacalls@admin.com", "default admin email created on first run (empty to disable)")
	seedAdminPass := flag.String("seed-admin-password", "admin", "default admin password created on first run")
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)

	// Resolve the SPA build directory even when an old systemd unit still passes
	// '-static client/dist'. If the supplied path is missing or has no index.html,
	// fall back to the current root build ('dist') and then the legacy build
	// ('client/dist'), checking both WorkingDirectory and the binary directory.
	resolvedStatic := resolveStaticDir(*staticDir)
	if resolvedStatic == "" {
		log.Warn("SPA build not found; browser routes will return 404 until the frontend is built", "static", *staticDir)
	} else if *staticDir != "" && filepath.Clean(resolvedStatic) != filepath.Clean(*staticDir) {
		log.Warn("static directory override was invalid; using detected SPA build", "requested", *staticDir, "using", resolvedStatic)
	}
	*staticDir = resolvedStatic

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv, err := newServer(ctx, *dbPath, *staticDir, *maxCalls, log)
	if err != nil {
		log.Error("startup failed", "err", err)
		os.Exit(1)
	}
	defer srv.sessions.disconnectAll()

	if *seedAdminEmail != "" {
		if created, err := srv.auth.SeedAdmin(ctx, *seedAdminEmail, *seedAdminPass); err != nil {
			log.Error("seed admin failed", "err", err)
		} else if created {
			log.Info("default admin created", "email", *seedAdminEmail)
		}
	}

	if err := srv.sessions.Restore(ctx); err != nil {
		log.Error("session restore failed", "err", err)
		os.Exit(1)
	}

	httpSrv := &http.Server{Addr: *addr, Handler: srv.routes()}
	go func() {
		log.Info("HTTP server listening", "addr", *addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server error", "err", err)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}

func resolveStaticDir(requested string) string {
	seen := map[string]bool{}
	candidates := make([]string, 0, 8)
	add := func(paths ...string) {
		for _, p := range paths {
			if p == "" {
				continue
			}
			clean := filepath.Clean(p)
			if !seen[clean] {
				seen[clean] = true
				candidates = append(candidates, clean)
			}
		}
	}

	add(requested)
	add("dist", "client/dist")
	if wd, err := os.Getwd(); err == nil {
		add(filepath.Join(wd, "dist"), filepath.Join(wd, "client", "dist"))
	}
	if exe, err := os.Executable(); err == nil {
		base := filepath.Dir(exe)
		add(filepath.Join(base, "dist"), filepath.Join(base, "client", "dist"))
	}

	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			if _, err := os.Stat(filepath.Join(candidate, "index.html")); err == nil {
				return candidate
			}
		}
	}
	return ""
}
