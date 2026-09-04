package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// Rotas das campanhas de mídia.
//
// O upload do arquivo reaproveita `POST /api/flow/assets`, que já grava em
// media/ e devolve a URL — não vale a pena um segundo caminho de upload para
// fazer exatamente a mesma coisa.

func (s *server) registerCampaignRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/campaigns", s.requireAuth(s.handleCampaignList))
	mux.HandleFunc("POST /api/campaigns", s.requireAuth(s.handleCampaignCreate))
	mux.HandleFunc("GET /api/campaigns/{id}", s.requireAuth(s.handleCampaignGet))
	mux.HandleFunc("PUT /api/campaigns/{id}", s.requireAuth(s.handleCampaignUpdate))
	mux.HandleFunc("DELETE /api/campaigns/{id}", s.requireAuth(s.handleCampaignDelete))
	mux.HandleFunc("GET /api/campaigns/{id}/targets", s.requireAuth(s.handleCampaignTargets))
	mux.HandleFunc("POST /api/campaigns/{id}/targets", s.requireAuth(s.handleCampaignAddTargets))
	mux.HandleFunc("DELETE /api/campaigns/{id}/targets", s.requireAuth(s.handleCampaignClearTargets))
	mux.HandleFunc("POST /api/campaigns/{id}/start", s.requireAuth(s.handleCampaignStart))
	mux.HandleFunc("POST /api/campaigns/{id}/pause", s.requireAuth(s.handleCampaignPause))
	mux.HandleFunc("GET /api/tags/{id}/chats", s.requireAuth(s.handleTagChats))
}

// handleTagChats devolve as conversas marcadas com uma tag, para a campanha
// montar a lista "todo mundo com a tag X".
func (s *server) handleTagChats(w http.ResponseWriter, r *http.Request) {
	if currentUserFromReq(r) == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	chats, err := s.tags.ChatsWithTag(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"chats": chats})
}

/** Carrega a campanha conferindo que ela é do usuário. */
func (s *server) campanhaDoUsuario(w http.ResponseWriter, r *http.Request) *CampaignRow {
	u := currentUserFromReq(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return nil
	}
	c, err := s.campaigns.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "campanha não encontrada"})
		return nil
	}
	if !u.IsAdmin() && c.OwnerID != "" && c.OwnerID != u.ID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "sem acesso"})
		return nil
	}
	return c
}

func (s *server) handleCampaignList(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	lista, err := s.campaigns.List(r.Context(), u.ID, u.IsAdmin())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// O progresso vem junto: a tela mostra "132 de 400" sem uma chamada por linha.
	saida := make([]map[string]any, 0, len(lista))
	for _, c := range lista {
		p, _ := s.campaigns.Progresso(r.Context(), c.ID)
		saida = append(saida, map[string]any{"campaign": c, "progress": p})
	}
	writeJSON(w, http.StatusOK, map[string]any{"campaigns": saida})
}

/** Valores de partida conservadores: melhor devagar do que com o número bloqueado. */
func aplicarPadroesDeRitmo(c *CampaignRow) {
	if c.MinIntervalSec <= 0 {
		c.MinIntervalSec = 25
	}
	if c.MaxIntervalSec <= c.MinIntervalSec {
		c.MaxIntervalSec = c.MinIntervalSec + 45
	}
	if c.PerHour < 0 {
		c.PerHour = 0
	}
	if c.PerDay < 0 {
		c.PerDay = 0
	}
	if c.WindowStart < 0 || c.WindowStart > 23 {
		c.WindowStart = 8
	}
	if c.WindowEnd < 0 || c.WindowEnd > 23 {
		c.WindowEnd = 20
	}
	if strings.TrimSpace(c.Weekdays) == "" {
		c.Weekdays = "1,2,3,4,5,6"
	}
	if strings.TrimSpace(c.Name) == "" {
		c.Name = "Campanha sem nome"
	}
}

func (s *server) handleCampaignCreate(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var c CampaignRow
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&c); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "corpo inválido"})
		return
	}
	c.OwnerID = u.ID
	c.Status = "draft"
	c.Warmup = true
	c.PerHour, c.PerDay = 40, 250
	// A janela precisa ser definida aqui, e não em aplicarPadroesDeRitmo: lá o
	// zero é um valor legítimo ("dia inteiro"), então uma campanha nova nascia
	// com 0 às 0 e podia disparar de madrugada.
	c.WindowStart, c.WindowEnd = 8, 20
	aplicarPadroesDeRitmo(&c)
	if err := s.campaigns.Create(r.Context(), &c); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *server) handleCampaignGet(w http.ResponseWriter, r *http.Request) {
	c := s.campanhaDoUsuario(w, r)
	if c == nil {
		return
	}
	p, _ := s.campaigns.Progresso(r.Context(), c.ID)
	writeJSON(w, http.StatusOK, map[string]any{"campaign": c, "progress": p})
}

func (s *server) handleCampaignUpdate(w http.ResponseWriter, r *http.Request) {
	atual := s.campanhaDoUsuario(w, r)
	if atual == nil {
		return
	}
	var novo CampaignRow
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&novo); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "corpo inválido"})
		return
	}
	// Identidade e estado de execução não se alteram por aqui: quem muda o
	// status são as rotas start/pause e o próprio disparador.
	novo.ID, novo.OwnerID = atual.ID, atual.OwnerID
	novo.Status, novo.StartedAt, novo.FinishedAt = atual.Status, atual.StartedAt, atual.FinishedAt
	novo.CreatedAt, novo.LastError = atual.CreatedAt, atual.LastError
	aplicarPadroesDeRitmo(&novo)
	if err := s.campaigns.Update(r.Context(), &novo); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, novo)
}

func (s *server) handleCampaignDelete(w http.ResponseWriter, r *http.Request) {
	c := s.campanhaDoUsuario(w, r)
	if c == nil {
		return
	}
	if err := s.campaigns.Delete(r.Context(), c.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "deleted"})
}

func (s *server) handleCampaignTargets(w http.ResponseWriter, r *http.Request) {
	c := s.campanhaDoUsuario(w, r)
	if c == nil {
		return
	}
	alvos, err := s.campaigns.Targets(r.Context(), c.ID, r.URL.Query().Get("status"), 2000)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"targets": alvos})
}

func (s *server) handleCampaignAddTargets(w http.ResponseWriter, r *http.Request) {
	c := s.campanhaDoUsuario(w, r)
	if c == nil {
		return
	}
	if c.Status == "running" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "pause a campanha antes de mexer na lista"})
		return
	}
	var body struct {
		Targets []CampaignTarget `json:"targets"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "corpo inválido"})
		return
	}
	n, err := s.campaigns.AddTargets(r.Context(), c.ID, body.Targets)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	p, _ := s.campaigns.Progresso(r.Context(), c.ID)
	// `adicionados` costuma ser menor que o enviado: repetido é ignorado, e o
	// usuário precisa ver isso para não achar que perdeu contato.
	writeJSON(w, http.StatusOK, map[string]any{"adicionados": n, "recebidos": len(body.Targets), "progress": p})
}

func (s *server) handleCampaignClearTargets(w http.ResponseWriter, r *http.Request) {
	c := s.campanhaDoUsuario(w, r)
	if c == nil {
		return
	}
	if c.Status == "running" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "pause a campanha antes de mexer na lista"})
		return
	}
	if err := s.campaigns.RemoveTargets(r.Context(), c.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "cleared"})
}

func (s *server) handleCampaignStart(w http.ResponseWriter, r *http.Request) {
	c := s.campanhaDoUsuario(w, r)
	if c == nil {
		return
	}
	if c.Text == "" && c.MediaURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "a campanha não tem mensagem nem arquivo"})
		return
	}
	p, _ := s.campaigns.Progresso(r.Context(), c.ID)
	if p.Pending == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "não há contatos pendentes nesta campanha"})
		return
	}
	// Sem número conectado o disparador pausaria no primeiro tique — melhor
	// recusar aqui, com um motivo que se entende.
	if s.campaignRunner != nil && !s.campaignRunner.algumConectado(*c) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "nenhum número conectado para disparar"})
		return
	}
	if err := s.campaigns.SetStatus(r.Context(), c.ID, "running", ""); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "running"})
}

func (s *server) handleCampaignPause(w http.ResponseWriter, r *http.Request) {
	c := s.campanhaDoUsuario(w, r)
	if c == nil {
		return
	}
	if err := s.campaigns.SetStatus(r.Context(), c.ID, "paused", ""); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "paused"})
}
