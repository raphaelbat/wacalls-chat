package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Disparador das campanhas de mídia.
//
// A parte que importa aqui não é enviar — é NÃO enviar rápido demais. Quem
// dispara mil mensagens seguidas do mesmo número perde o número. As regras:
//
//   - rodízio: alterna entre os números conectados, sempre pegando o que está
//     há mais tempo sem disparar;
//   - intervalo aleatório entre um envio e o próximo, nunca fixo (cadência
//     perfeita é a assinatura mais óbvia de robô);
//   - teto por hora e por dia, contados POR NÚMERO e guardados no banco, então
//     reiniciar o servidor não zera o contador;
//   - janela de horário e dias da semana;
//   - aquecimento: número novo começa com pouco e vai soltando ao longo de uma
//     semana;
//   - número que cai sai do rodízio sozinho, e a campanha pausa se todos caírem.
//
// Nada disso torna o bloqueio impossível. O que de fato protege é a lista ser
// de gente que pediu contato: o WhatsApp bloqueia sobretudo por denúncia de
// quem recebe. Ritmo reduz risco, não elimina.

type campaignRunner struct {
	store  *campaignStore
	bridge *FlowBridge
	mgr    *SessionManager
	log    *slog.Logger

	mu sync.Mutex
	// Quando cada campanha pode disparar de novo, e qual número usou por
	// último — o rodízio é por campanha, não global.
	proximoEnvio map[string]time.Time
	ultimoNumero map[string]string
}

func newCampaignRunner(store *campaignStore, bridge *FlowBridge, mgr *SessionManager, log *slog.Logger) *campaignRunner {
	return &campaignRunner{
		store: store, bridge: bridge, mgr: mgr, log: log,
		proximoEnvio: map[string]time.Time{},
		ultimoNumero: map[string]string{},
	}
}

// ---------------------------------------------------------------------------
// Regras de ritmo — funções puras, para poderem ser testadas sem banco nem rede
// ---------------------------------------------------------------------------

// dentroDaJanela diz se dá para disparar neste momento.
//
// Janela que "vira a meia-noite" (das 20h às 6h) é aceita: quando o início é
// maior que o fim, a faixa é entendida como atravessando o dia.
func dentroDaJanela(agora time.Time, inicio, fim int, diasDaSemana string) bool {
	if !diaPermitido(agora, diasDaSemana) {
		return false
	}
	// Janela zerada ou cobrindo o dia inteiro: sem restrição de hora.
	if inicio == fim {
		return true
	}
	h := agora.Hour()
	if inicio < fim {
		return h >= inicio && h < fim
	}
	return h >= inicio || h < fim
}

func diaPermitido(agora time.Time, diasDaSemana string) bool {
	dias := strings.TrimSpace(diasDaSemana)
	if dias == "" {
		return true
	}
	hoje := int(agora.Weekday()) // 0 = domingo
	for _, p := range strings.Split(dias, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil && n == hoje {
			return true
		}
	}
	return false
}

// tetoDeAquecimento devolve quantas mensagens um número pode mandar hoje.
//
// Número recém-conectado que dispara 300 mensagens no primeiro dia é o caso
// clássico de bloqueio. A escala abaixo sobe ao longo de uma semana e nunca
// passa do teto que o usuário configurou.
func tetoDeAquecimento(diasDeUso int, tetoConfigurado int) int {
	if tetoConfigurado <= 0 {
		tetoConfigurado = 250
	}
	escala := []int{20, 40, 60, 90, 130, 180, 240}
	if diasDeUso < 0 {
		diasDeUso = 0
	}
	if diasDeUso >= len(escala) {
		return tetoConfigurado
	}
	if escala[diasDeUso] < tetoConfigurado {
		return escala[diasDeUso]
	}
	return tetoConfigurado
}

// intervaloAleatorio sorteia a pausa até o próximo envio.
func intervaloAleatorio(minSeg, maxSeg int) time.Duration {
	if minSeg < 1 {
		minSeg = 1
	}
	if maxSeg <= minSeg {
		maxSeg = minSeg + 1
	}
	return time.Duration(minSeg+rand.Intn(maxSeg-minSeg+1)) * time.Second
}

// candidato é um número disponível para o rodízio.
type candidato struct {
	sessionID string
	usadoHoje int
	usadoHora int
	tetoDia   int
	tetoHora  int
	ultimoUso int64
	conectado bool
}

func (c candidato) disponivel() bool {
	if !c.conectado {
		return false
	}
	if c.tetoHora > 0 && c.usadoHora >= c.tetoHora {
		return false
	}
	if c.tetoDia > 0 && c.usadoHoje >= c.tetoDia {
		return false
	}
	return true
}

// escolherNumero faz o rodízio: entre os disponíveis, pega o que está há mais
// tempo parado. Empate resolve pelo id, só para a escolha ser previsível em
// teste.
func escolherNumero(cands []candidato) (string, bool) {
	melhor := ""
	var melhorUso int64 = 1<<62 - 1
	for _, c := range cands {
		if !c.disponivel() {
			continue
		}
		if c.ultimoUso < melhorUso || (c.ultimoUso == melhorUso && c.sessionID < melhor) {
			melhor, melhorUso = c.sessionID, c.ultimoUso
		}
	}
	return melhor, melhor != ""
}

// ---------------------------------------------------------------------------
// Laço
// ---------------------------------------------------------------------------

// Start liga o disparador. Um tique por segundo é suficiente: os intervalos
// entre envios são de dezenas de segundos.
func (r *campaignRunner) Start(ctx context.Context) {
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		limpeza := time.NewTicker(6 * time.Hour)
		defer limpeza.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-limpeza.C:
				_ = r.store.LimparEnviosAntigos(ctx)
			case <-tick.C:
				r.rodada(ctx)
			}
		}
	}()
}

func (r *campaignRunner) rodada(ctx context.Context) {
	campanhas, err := r.store.Rodando(ctx)
	if err != nil {
		r.log.Error("campanhas: nao consegui listar", "err", err)
		return
	}
	for _, c := range campanhas {
		r.passo(ctx, c)
	}
}

/** Um envio, no máximo, por campanha por tique. */
func (r *campaignRunner) passo(ctx context.Context, c CampaignRow) {
	agora := time.Now()

	r.mu.Lock()
	quando, temHora := r.proximoEnvio[c.ID]
	r.mu.Unlock()
	if temHora && agora.Before(quando) {
		return
	}

	if !dentroDaJanela(agora, c.WindowStart, c.WindowEnd, c.Weekdays) {
		// Fora da janela a campanha dorme; volta sozinha no horário.
		r.adiar(c.ID, 5*time.Minute)
		return
	}

	alvo, err := r.store.ProximoPendente(ctx, c.ID)
	if err != nil {
		r.log.Error("campanhas: fila", "campanha", c.ID, "err", err)
		r.adiar(c.ID, time.Minute)
		return
	}
	if alvo == nil {
		_ = r.store.SetStatus(ctx, c.ID, "finished", "")
		r.log.Info("campanha concluida", "campanha", c.ID, "nome", c.Name)
		return
	}

	sessionID, ok := r.numeroDisponivel(ctx, c)
	if !ok {
		// Ou todos bateram o teto, ou caíram. Nos dois casos é esperar — mas se
		// nenhum estiver conectado, pausar é mais honesto do que fingir que a
		// campanha continua andando.
		if !r.algumConectado(c) {
			_ = r.store.SetStatus(ctx, c.ID, "paused", "nenhum número conectado")
			r.log.Warn("campanha pausada: nenhum numero conectado", "campanha", c.ID)
			return
		}
		r.adiar(c.ID, 2*time.Minute)
		return
	}

	if err := r.enviar(ctx, c, sessionID, *alvo); err != nil {
		_ = r.store.MarcarAlvo(ctx, alvo.ID, "failed", sessionID, err.Error())
		r.log.Warn("campanha: envio falhou", "campanha", c.ID, "para", alvo.JID, "err", err)
	} else {
		_ = r.store.MarcarAlvo(ctx, alvo.ID, "sent", sessionID, "")
		_ = r.store.RegistrarEnvio(ctx, sessionID)
	}

	r.mu.Lock()
	r.ultimoNumero[c.ID] = sessionID
	r.mu.Unlock()
	r.adiar(c.ID, intervaloAleatorio(c.MinIntervalSec, c.MaxIntervalSec))
}

func (r *campaignRunner) adiar(campaignID string, d time.Duration) {
	r.mu.Lock()
	r.proximoEnvio[campaignID] = time.Now().Add(d)
	r.mu.Unlock()
}

/** Números que a campanha pode usar, já com estado de conexão. */
func (r *campaignRunner) numerosDaCampanha(c CampaignRow) []SessionInfo {
	infos := r.mgr.infos()
	escolhidos := strings.TrimSpace(c.SessionIDs)
	if escolhidos == "" {
		return infos
	}
	permitido := map[string]bool{}
	for _, id := range strings.Split(escolhidos, ",") {
		if id = strings.TrimSpace(id); id != "" {
			permitido[id] = true
		}
	}
	out := []SessionInfo{}
	for _, i := range infos {
		if permitido[i.ID] {
			out = append(out, i)
		}
	}
	return out
}

func (r *campaignRunner) algumConectado(c CampaignRow) bool {
	for _, i := range r.numerosDaCampanha(c) {
		if i.State == "open" {
			return true
		}
	}
	return false
}

func (r *campaignRunner) numeroDisponivel(ctx context.Context, c CampaignRow) (string, bool) {
	agora := time.Now()
	inicioDoDia := time.Date(agora.Year(), agora.Month(), agora.Day(), 0, 0, 0, 0, agora.Location()).Unix()
	umaHoraAtras := agora.Add(-time.Hour).Unix()

	r.mu.Lock()
	ultimo := r.ultimoNumero[c.ID]
	r.mu.Unlock()

	cands := []candidato{}
	for _, info := range r.numerosDaCampanha(c) {
		dia, _ := r.store.EnviosDesde(ctx, info.ID, inicioDoDia)
		hora, _ := r.store.EnviosDesde(ctx, info.ID, umaHoraAtras)

		tetoDia := c.PerDay
		if c.Warmup {
			primeiro, _ := r.store.PrimeiroEnvio(ctx, info.ID)
			dias := 0
			if primeiro > 0 {
				dias = int(agora.Sub(time.Unix(primeiro, 0)).Hours() / 24)
			}
			tetoDia = tetoDeAquecimento(dias, c.PerDay)
		}

		// O que acabou de disparar vai para o fim da fila do rodízio.
		var ultimoUso int64
		if info.ID == ultimo {
			ultimoUso = agora.Unix()
		}

		cands = append(cands, candidato{
			sessionID: info.ID,
			usadoHoje: dia, usadoHora: hora,
			tetoDia: tetoDia, tetoHora: c.PerHour,
			ultimoUso: ultimoUso,
			conectado: info.State == "open",
		})
	}
	return escolherNumero(cands)
}

func (r *campaignRunner) enviar(ctx context.Context, c CampaignRow, sessionID string, alvo CampaignTarget) error {
	if r.bridge == nil {
		return fmt.Errorf("envio indisponivel")
	}
	texto := personalizar(c.Text, alvo)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if c.MediaURL != "" {
		kind := c.MediaKind
		if kind == "" {
			kind = "image"
		}
		return r.bridge.SendWhatsAppMedia(ctx, sessionID, alvo.JID, kind, c.MediaURL, texto, c.Filename)
	}
	if strings.TrimSpace(texto) == "" {
		return fmt.Errorf("campanha sem texto e sem arquivo")
	}
	return r.bridge.SendWhatsAppText(ctx, sessionID, alvo.JID, texto)
}

// personalizar troca os marcadores simples do texto da campanha.
//
// Aqui não vale a pena o text/template dos fluxos: são dois campos, e um erro
// de sintaxe no meio de um disparo em massa sairia caro. Uma troca direta
// nunca falha.
func personalizar(texto string, alvo CampaignTarget) string {
	nome := strings.TrimSpace(alvo.Name)
	primeiro := nome
	if i := strings.IndexByte(nome, ' '); i > 0 {
		primeiro = nome[:i]
	}
	rep := strings.NewReplacer(
		"{{nome}}", nome,
		"{{primeiro_nome}}", primeiro,
		"{nome}", nome,
		"{primeiro_nome}", primeiro,
	)
	return rep.Replace(texto)
}
