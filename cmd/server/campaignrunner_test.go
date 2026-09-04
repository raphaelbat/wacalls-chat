package main

import (
	"testing"
	"time"
)

// A parte que protege o número é o ritmo. Se ele estiver errado, a campanha
// funciona igual — até o número cair. Por isso essas regras são testadas
// sozinhas, sem banco e sem rede.

func TestJanelaDeHorario(t *testing.T) {
	// Segunda-feira, 2 de setembro de 2026.
	seg := func(h int) time.Time { return time.Date(2026, 9, 2, h, 30, 0, 0, time.UTC) }
	todosOsDias := "0,1,2,3,4,5,6"

	casos := []struct {
		nome        string
		hora        int
		inicio, fim int
		dias        string
		esperado    bool
	}{
		{"dentro do horário comercial", 14, 8, 20, todosOsDias, true},
		{"antes de abrir", 6, 8, 20, todosOsDias, false},
		{"depois de fechar", 21, 8, 20, todosOsDias, false},
		{"na hora de abrir entra", 8, 8, 20, todosOsDias, true},
		{"na hora de fechar não entra", 20, 8, 20, todosOsDias, false},
		{"janela virando a meia-noite, de noite", 22, 20, 6, todosOsDias, true},
		{"janela virando a meia-noite, de madrugada", 3, 20, 6, todosOsDias, true},
		{"janela virando a meia-noite, de tarde", 15, 20, 6, todosOsDias, false},
		{"início igual ao fim libera o dia inteiro", 3, 0, 0, todosOsDias, true},
		{"dia da semana não permitido", 14, 8, 20, "0,6", false},
		{"lista de dias vazia permite todos", 14, 8, 20, "", true},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := dentroDaJanela(seg(c.hora), c.inicio, c.fim, c.dias); got != c.esperado {
				t.Fatalf("dentroDaJanela(%dh) = %v, queria %v", c.hora, got, c.esperado)
			}
		})
	}
}

// Domingo é dia 6 de setembro de 2026 — confere que o índice do dia bate.
func TestDiaDaSemanaUsaDomingoComoZero(t *testing.T) {
	domingo := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	if domingo.Weekday() != time.Sunday {
		t.Fatalf("a data escolhida não é domingo, e sim %v", domingo.Weekday())
	}
	if !diaPermitido(domingo, "0") {
		t.Fatal("domingo deveria ser o dia 0")
	}
	if diaPermitido(domingo, "1,2,3,4,5,6") {
		t.Fatal("a lista de segunda a sábado não pode aceitar domingo")
	}
}

func TestAquecimentoSobeAoLongoDaSemana(t *testing.T) {
	teto := 250
	anterior := 0
	for dia := 0; dia < 7; dia++ {
		atual := tetoDeAquecimento(dia, teto)
		if atual <= anterior {
			t.Fatalf("dia %d liberou %d, não subiu em relação a %d", dia, atual, anterior)
		}
		if atual > teto {
			t.Fatalf("dia %d liberou %d, acima do teto configurado (%d)", dia, atual, teto)
		}
		anterior = atual
	}
	if got := tetoDeAquecimento(30, teto); got != teto {
		t.Fatalf("depois de um mês o aquecimento devia sair do caminho, mas liberou %d", got)
	}
}

// O aquecimento nunca pode AUMENTAR o teto que o usuário escolheu.
func TestAquecimentoNuncaPassaDoTetoEscolhido(t *testing.T) {
	if got := tetoDeAquecimento(0, 10); got != 10 {
		t.Fatalf("com teto de 10 no primeiro dia liberou %d", got)
	}
	if got := tetoDeAquecimento(6, 15); got != 15 {
		t.Fatalf("com teto de 15 no sétimo dia liberou %d", got)
	}
}

func TestIntervaloFicaDentroDaFaixa(t *testing.T) {
	for i := 0; i < 400; i++ {
		d := intervaloAleatorio(25, 70)
		if d < 25*time.Second || d > 70*time.Second {
			t.Fatalf("sorteou %v, fora de 25s..70s", d)
		}
	}
	// Faixa inválida não pode virar envio instantâneo em rajada.
	if d := intervaloAleatorio(0, 0); d < time.Second {
		t.Fatalf("faixa zerada devolveu %v", d)
	}
	if d := intervaloAleatorio(60, 10); d < 60*time.Second {
		t.Fatalf("máximo menor que o mínimo devolveu %v", d)
	}
}

func TestRodizioPegaONumeroMaisParado(t *testing.T) {
	cands := []candidato{
		{sessionID: "a", conectado: true, ultimoUso: 500, tetoDia: 100, tetoHora: 10},
		{sessionID: "b", conectado: true, ultimoUso: 100, tetoDia: 100, tetoHora: 10},
		{sessionID: "c", conectado: true, ultimoUso: 300, tetoDia: 100, tetoHora: 10},
	}
	got, ok := escolherNumero(cands)
	if !ok || got != "b" {
		t.Fatalf("escolheu %q (ok=%v), queria o mais parado (b)", got, ok)
	}
}

func TestRodizioPulaNumeroDesconectado(t *testing.T) {
	cands := []candidato{
		{sessionID: "caiu", conectado: false, ultimoUso: 0, tetoDia: 100, tetoHora: 10},
		{sessionID: "ok", conectado: true, ultimoUso: 900, tetoDia: 100, tetoHora: 10},
	}
	got, ok := escolherNumero(cands)
	if !ok || got != "ok" {
		t.Fatalf("escolheu %q, mas o número desconectado devia ser pulado", got)
	}
}

func TestRodizioRespeitaOsTetos(t *testing.T) {
	casos := []struct {
		nome string
		c    candidato
	}{
		{"bateu o teto da hora", candidato{sessionID: "x", conectado: true, usadoHora: 10, tetoHora: 10, tetoDia: 100}},
		{"bateu o teto do dia", candidato{sessionID: "x", conectado: true, usadoHoje: 250, tetoDia: 250, tetoHora: 0}},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			if _, ok := escolherNumero([]candidato{caso.c}); ok {
				t.Fatal("o número devia estar indisponível")
			}
		})
	}
}

// Teto zero quer dizer "sem teto", não "não pode mandar nada".
func TestTetoZeradoSignificaSemLimite(t *testing.T) {
	c := candidato{sessionID: "x", conectado: true, usadoHoje: 9999, usadoHora: 9999, tetoDia: 0, tetoHora: 0}
	if _, ok := escolherNumero([]candidato{c}); !ok {
		t.Fatal("com teto zerado o número devia continuar disponível")
	}
}

func TestSemNumeroDisponivelNaoEscolheNada(t *testing.T) {
	if _, ok := escolherNumero(nil); ok {
		t.Fatal("lista vazia não pode devolver número")
	}
	todosCaidos := []candidato{{sessionID: "a"}, {sessionID: "b"}}
	if _, ok := escolherNumero(todosCaidos); ok {
		t.Fatal("com todos desconectados não pode devolver número")
	}
}

// O rodízio precisa alternar de fato. Simula dez envios seguidos entre dois
// números, marcando o recém-usado como "usado agora" — que é o que o runner faz.
func TestRodizioAlternaEntreDoisNumeros(t *testing.T) {
	usoA, usoB := int64(0), int64(0)
	sequencia := []string{}
	for i := 0; i < 10; i++ {
		got, ok := escolherNumero([]candidato{
			{sessionID: "a", conectado: true, ultimoUso: usoA, tetoDia: 100, tetoHora: 100},
			{sessionID: "b", conectado: true, ultimoUso: usoB, tetoDia: 100, tetoHora: 100},
		})
		if !ok {
			t.Fatal("devia ter número disponível")
		}
		sequencia = append(sequencia, got)
		if got == "a" {
			usoA = int64(i + 1)
		} else {
			usoB = int64(i + 1)
		}
	}
	for i := 1; i < len(sequencia); i++ {
		if sequencia[i] == sequencia[i-1] {
			t.Fatalf("o mesmo número disparou duas vezes seguidas: %v", sequencia)
		}
	}
}

func TestPersonalizacaoDoTexto(t *testing.T) {
	alvo := CampaignTarget{Name: "Maria Silva Souza"}
	casos := map[string]string{
		"Oi {{nome}}":                  "Oi Maria Silva Souza",
		"Oi {{primeiro_nome}}":         "Oi Maria",
		"Oi {nome}, tudo bem?":         "Oi Maria Silva Souza, tudo bem?",
		"Sem marcador nenhum":          "Sem marcador nenhum",
		"{{primeiro_nome}} e {{nome}}": "Maria e Maria Silva Souza",
	}
	for entrada, esperado := range casos {
		if got := personalizar(entrada, alvo); got != esperado {
			t.Errorf("personalizar(%q) = %q, queria %q", entrada, got, esperado)
		}
	}
}

// Contato sem nome não pode virar "Oi ," na mensagem do cliente.
func TestPersonalizacaoComContatoSemNome(t *testing.T) {
	got := personalizar("Oi {{primeiro_nome}}", CampaignTarget{Name: ""})
	if got != "Oi " {
		t.Fatalf("com nome vazio saiu %q", got)
	}
}
