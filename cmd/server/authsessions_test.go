package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// Com o limite aberto (WACALLS_MAX_SESSOES), PC e celular convivem.
func TestSessoesSimultaneasDoMesmoUsuario(t *testing.T) {
	t.Setenv("WACALLS_MAX_SESSOES", "5")
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("abrindo banco: %v", err)
	}
	defer db.Close()

	store, err := newAuthStore(ctx, db)
	if err != nil {
		t.Fatalf("criando store: %v", err)
	}
	if _, err := store.SeedAdmin(ctx, "dono@exemplo.com", "senha-boa"); err != nil {
		t.Fatalf("criando admin: %v", err)
	}

	// Aba 1 e aba 2, o mesmo usuario.
	_, tok1, err := store.Login(ctx, "dono@exemplo.com", "senha-boa")
	if err != nil {
		t.Fatalf("login 1: %v", err)
	}
	_, tok2, err := store.Login(ctx, "dono@exemplo.com", "senha-boa")
	if err != nil {
		t.Fatalf("login 2: %v", err)
	}
	if tok1 == tok2 {
		t.Fatal("o segundo login devolveu o mesmo token")
	}
	for nome, tok := range map[string]string{"primeira aba": tok1, "segunda aba": tok2} {
		if _, err := store.UserByToken(ctx, tok); err != nil {
			t.Fatalf("%s foi derrubada: %v", nome, err)
		}
	}
}

// Mas a senha nao pode correr solta: passando do limite, a sessao mais antiga
// cai e o hub SSE e avisado so dela.
func TestSessaoMaisAntigaCaiNoLimite(t *testing.T) {
	t.Setenv("WACALLS_MAX_SESSOES", "5")
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("abrindo banco: %v", err)
	}
	defer db.Close()

	store, err := newAuthStore(ctx, db)
	if err != nil {
		t.Fatalf("criando store: %v", err)
	}
	revogados := make(chan []string, 8)
	store.OnTokensRevoked = func(tokens []string) { revogados <- tokens }
	if _, err := store.SeedAdmin(ctx, "dono@exemplo.com", "senha-boa"); err != nil {
		t.Fatalf("criando admin: %v", err)
	}

	tokens := make([]string, 0, maxSessoesPorUsuario()+1)
	for i := 0; i <= maxSessoesPorUsuario(); i++ {
		_, tok, err := store.Login(ctx, "dono@exemplo.com", "senha-boa")
		if err != nil {
			t.Fatalf("login %d: %v", i, err)
		}
		tokens = append(tokens, tok)
	}

	if _, err := store.UserByToken(ctx, tokens[0]); err == nil {
		t.Fatal("a sessao mais antiga deveria ter caido ao passar do limite")
	}
	for i := 1; i < len(tokens); i++ {
		if _, err := store.UserByToken(ctx, tokens[i]); err != nil {
			t.Fatalf("sessao %d deveria continuar valida: %v", i, err)
		}
	}

	var vivas int
	if err := db.QueryRow(`SELECT COUNT(1) FROM auth_tokens`).Scan(&vivas); err != nil {
		t.Fatalf("contando sessoes: %v", err)
	}
	if vivas != maxSessoesPorUsuario() {
		t.Fatalf("sobraram %d sessoes, esperado %d", vivas, maxSessoesPorUsuario())
	}

	select {
	case avisados := <-revogados:
		if len(avisados) != 1 || avisados[0] != tokens[0] {
			t.Fatalf("o aviso de revogacao saiu errado: %v", avisados)
		}
	default:
		t.Fatal("ninguem foi avisado da sessao derrubada")
	}
}

// O padrao e sessao unica: entrar em OUTRO navegador derruba o anterior. E o
// que impede a senha de circular pela empresa inteira.
func TestPadraoDerrubaOAcessoAnterior(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("abrindo banco: %v", err)
	}
	defer db.Close()

	store, err := newAuthStore(ctx, db)
	if err != nil {
		t.Fatalf("criando store: %v", err)
	}
	if _, err := store.SeedAdmin(ctx, "dono@exemplo.com", "senha-boa"); err != nil {
		t.Fatalf("criando admin: %v", err)
	}

	_, antigo, _ := store.Login(ctx, "dono@exemplo.com", "senha-boa")
	_, novo, _ := store.Login(ctx, "dono@exemplo.com", "senha-boa")

	if _, err := store.UserByToken(ctx, antigo); err == nil {
		t.Fatal("o acesso anterior deveria ter sido derrubado")
	}
	if _, err := store.UserByToken(ctx, novo); err != nil {
		t.Fatalf("o acesso novo deveria valer: %v", err)
	}
}

// A consulta mostra quem esta logado, e da para derrubar um acesso pelo inicio
// do token — sem que o token inteiro precise sair do servidor.
func TestConsultaEEncerramentoDeAcessos(t *testing.T) {
	t.Setenv("WACALLS_MAX_SESSOES", "5")
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("abrindo banco: %v", err)
	}
	defer db.Close()

	store, err := newAuthStore(ctx, db)
	if err != nil {
		t.Fatalf("criando store: %v", err)
	}
	if _, err := store.SeedAdmin(ctx, "dono@exemplo.com", "senha-boa"); err != nil {
		t.Fatalf("criando admin: %v", err)
	}

	_, tokPC, _ := store.Login(ctx, "dono@exemplo.com", "senha-boa")
	store.MarcarAcesso(ctx, tokPC, "Mozilla/5.0 (Windows NT 10.0) Chrome/140", "192.168.0.10")
	_, tokCel, _ := store.Login(ctx, "dono@exemplo.com", "senha-boa")
	store.MarcarAcesso(ctx, tokCel, "Mozilla/5.0 (Linux; Android 14) Chrome/140 Mobile", "192.168.0.22")

	lista, err := store.ListarAcessos(ctx, "")
	if err != nil {
		t.Fatalf("listando: %v", err)
	}
	if len(lista) != 2 {
		t.Fatalf("esperava 2 acessos, veio %d", len(lista))
	}
	achouCelular := false
	for _, a := range lista {
		if a.Email != "dono@exemplo.com" {
			t.Fatalf("e-mail errado na listagem: %q", a.Email)
		}
		if a.IP == "" || a.Navegador == "" || a.Desde == 0 {
			t.Fatalf("acesso sem origem: %+v", a)
		}
		if strings.Contains(a.Navegador, "Android") {
			achouCelular = true
		}
	}
	if !achouCelular {
		t.Fatal("o acesso do celular nao apareceu na consulta")
	}

	// Derruba o do PC pelo inicio do token, do jeito que a tela faz.
	revogados := make(chan []string, 2)
	store.OnTokensRevoked = func(tokens []string) { revogados <- tokens }
	if _, err := store.EncerrarAcesso(ctx, tokPC[:8], ""); err != nil {
		t.Fatalf("encerrando: %v", err)
	}
	if _, err := store.UserByToken(ctx, tokPC); err == nil {
		t.Fatal("o acesso encerrado ainda vale")
	}
	if _, err := store.UserByToken(ctx, tokCel); err != nil {
		t.Fatalf("o outro acesso nao podia cair junto: %v", err)
	}
	select {
	case avisados := <-revogados:
		if len(avisados) != 1 || avisados[0] != tokPC {
			t.Fatalf("aviso de revogacao errado: %v", avisados)
		}
	default:
		t.Fatal("o navegador derrubado nao foi avisado")
	}

	// Um atendente nao pode encerrar a sessao de outra pessoa.
	_, outroTok, _ := store.Login(ctx, "dono@exemplo.com", "senha-boa")
	if _, err := store.EncerrarAcesso(ctx, outroTok[:8], "id-de-outro-usuario"); err == nil {
		t.Fatal("dono errado conseguiu encerrar a sessao alheia")
	}
}
