package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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
	listUsers := flag.Bool("list-users", false, "lista os usuarios cadastrados e sai")
	resetAdminEmail := flag.String("reset-admin-email", "", "cria ou redefine este usuario como admin e sai")
	resetAdminPass := flag.String("reset-admin-password", "", "senha usada junto com -reset-admin-email")
	licenseFile := flag.String("license", "", "arquivo licenca.json (padrao: ao lado do binario)")
	licenseInfo := flag.Bool("license-info", false, "mostra o estado da licenca e o codigo desta maquina, e sai")
	flag.Parse()

	if *licenseInfo {
		printLicenseInfo(*licenseFile)
		return
	}

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

	// Ativacao: so bloqueia quando WACALLS_LICENSE_REQUIRED=1 (versao paga).
	licenseFlagPath = *licenseFile
	checkLicense(*licenseFile, licensePubKey(), log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv, err := newServer(ctx, *dbPath, *staticDir, *maxCalls, log)
	if err != nil {
		log.Error("startup failed", "err", err)
		os.Exit(1)
	}
	defer srv.sessions.disconnectAll()

	// Modo manutencao: roda o comando pedido e sai, sem subir o HTTP nem
	// reconectar as sessoes do WhatsApp.
	if *listUsers || *resetAdminEmail != "" {
		if err := runMaintenance(ctx, srv, *listUsers, *resetAdminEmail, *resetAdminPass); err != nil {
			log.Error("comando de manutencao falhou", "err", err)
			os.Exit(1)
		}
		return
	}

	if *seedAdminEmail != "" {
		if created, err := srv.auth.SeedAdmin(ctx, *seedAdminEmail, *seedAdminPass); err != nil {
			// Sem admin criado ninguem consegue entrar: log com a causa e a saida.
			log.Error("nao consegui criar o admin padrao; ninguem vai conseguir entrar ate isso ser resolvido",
				"email", *seedAdminEmail, "err", err,
				"dica", "crie a conta na mao com -reset-admin-email e -reset-admin-password")
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

// runMaintenance atende os comandos administrativos de linha de comando
// (-list-users e -reset-admin-email). Serve para destravar instalacoes onde
// ninguem consegue entrar: mostra quais contas existem e permite criar ou
// redefinir um admin sem depender do painel nem de e-mail de recuperacao.
func runMaintenance(ctx context.Context, srv *server, list bool, email, password string) error {
	users, err := srv.auth.ListUsers(ctx)
	if err != nil {
		return err
	}

	if list {
		fmt.Printf("%-36s %-14s %-7s %s\n", "EMAIL", "PERFIL", "ATIVO", "CRIADO EM")
		for _, u := range users {
			roles := strings.Join(u.Roles, ",")
			if roles == "" {
				roles = "-"
			}
			ativo := "nao"
			if u.Active {
				ativo = "sim"
			}
			fmt.Printf("%-36s %-14s %-7s %s\n", u.Email, roles, ativo,
				time.Unix(u.CreatedAt, 0).Format("2006-01-02 15:04"))
		}
		fmt.Printf("\ntotal: %d usuario(s)\n", len(users))
		if len(users) == 0 {
			fmt.Println("nenhum usuario cadastrado — use -reset-admin-email e -reset-admin-password para criar o primeiro admin")
		}
	}

	if email == "" {
		return nil
	}
	if password == "" {
		return errors.New("informe -reset-admin-password junto com -reset-admin-email")
	}
	if len(password) < 8 {
		fmt.Println("aviso: senha com menos de 8 caracteres - o painel nao aceitaria, mas aqui vale (uso local)")
	}

	target := normalizeEmail(email)
	for _, u := range users {
		if u.Email != target {
			continue
		}
		if err := srv.auth.SetPasswordRaw(ctx, u.ID, password); err != nil {
			return err
		}
		if err := srv.auth.SetRole(ctx, u.ID, RoleAdmin, true); err != nil {
			return err
		}
		if err := srv.auth.SetActive(ctx, u.ID, true); err != nil {
			return err
		}
		fmt.Printf("senha redefinida e perfil admin garantido para %s\n", target)
		return nil
	}

	if err := srv.auth.createAdminRaw(ctx, target, password); err != nil {
		return err
	}
	fmt.Printf("admin %s criado\n", target)
	return nil
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
