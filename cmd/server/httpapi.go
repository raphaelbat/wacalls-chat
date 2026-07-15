package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"wacalls/internal/voip/core"

	"go.mau.fi/whatsmeow/types"
)

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()

	// Public auth endpoints
	s.registerAuthRoutes(mux)

	// Authenticated endpoints
	mux.HandleFunc("GET /api/sessions", s.requireAuth(s.handleSessionList))
	mux.HandleFunc("POST /api/sessions", s.requireAuth(s.handleSessionCreate))
	mux.HandleFunc("PUT /api/sessions/{sid}", s.requireAuth(s.handleSessionUpdate))
	mux.HandleFunc("POST /api/sessions/{sid}/token", s.requireAuth(s.handleSessionRegenToken))
	mux.HandleFunc("DELETE /api/sessions/{sid}", s.requireAuth(s.handleSessionDelete))
	mux.HandleFunc("POST /api/sessions/{sid}/logout", s.requireAuth(s.handleSessionLogout))
	mux.HandleFunc("POST /api/sessions/{sid}/pair", s.requireAuth(s.handleSessionPair))
	mux.HandleFunc("POST /api/sessions/{sid}/calls", s.requireAuth(s.handleStartCall))
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/webrtc", s.requireAuth(s.handleWebRTC))
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/accept", s.requireAuth(s.handleAccept))
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/reject", s.requireAuth(s.handleReject))
	mux.HandleFunc("DELETE /api/sessions/{sid}/calls/{id}", s.requireAuth(s.handleEndCall))
	mux.HandleFunc("GET /api/sessions/{sid}/history", s.requireAuth(s.handleHistory))
	// Cross-session call history (used by /history page).
	mux.HandleFunc("GET /api/calls", s.requireAuth(s.handleListCalls))
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/recording", s.requireAuth(s.handleUploadRecording))
	mux.HandleFunc("POST /api/sessions/{sid}/calls/{id}/recording/sign", s.requireAuth(s.handleSignRecording))
	mux.HandleFunc("GET /api/recordings/{token}", s.handleDownloadRecording) // requires signed URL or owner cookie

	mux.HandleFunc("GET /api/events", s.handleEvents) // SSE handles its own auth

	s.registerFlowRoutes(mux)
	s.registerMessageRoutes(mux)
	s.registerQueueRoutes(mux)
	s.registerTagRoutes(mux)
	s.registerKanbanRoutes(mux)
	s.registerContactsRoutes(mux)
	s.registerSettingsRoutes(mux)

	s.registerBillingRoutes(mux)
	s.registerFreeTierRoutes(mux)
	s.registerFreeTierNotifyRoutes(mux)
	s.registerPasswordResetRoutes(mux)
	s.registerEmailVerificationRoutes(mux)
	s.registerHealthRoutes(mux)
	s.registerCloudAPIRoutes(mux)
	s.registerCallControlRoutes(mux)
	s.registerReportRoutes(mux)

	// Serve recorded media (flow record_audio node + future uploads).
	_ = os.MkdirAll("media", 0o755)
	// IMPORTANT: media files (gravações de URA, uploads de fluxo, etc.) podem
	// conter áudio sensível do cliente — exigimos sessão autenticada para
	// servir. Quem precisar de URL pública usa o fluxo /api/recordings/{token}
	// com assinatura HMAC dedicado.
	mediaFS := http.StripPrefix("/api/media/", safeFileServer(http.Dir("media")))
	mux.Handle("GET /api/media/", s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		mediaFS.ServeHTTP(w, r)
	}))

	// Always register the browser fallback. If the configured path is wrong or
	// temporarily missing, spaHandler returns a clear static-build 404 instead of
	// the bare net/http "404 page not found". Valid SPA routes like /connections
	// are served from index.html whenever the build exists.
	mux.Handle("/", spaHandler(s.staticDir))
	return withSecurityHeaders(withCORS(mux))
}

// spaHandler serves static files and falls back to index.html for client-side routes.
func spaHandler(dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}

		p := r.URL.Path
		// Do not serve the React app for unknown API/media routes. Registered API
		// handlers are matched before this fallback by ServeMux.
		if p == "/api" || strings.HasPrefix(p, "/api/") {
			http.NotFound(w, r)
			return
		}

		if dir == "" {
			http.Error(w, "SPA build not found. Run: cd client && npm install && npm run build, then restart wacalls.", http.StatusNotFound)
			return
		}

		index := filepath.Join(dir, "index.html")
		if _, err := os.Stat(index); err != nil {
			http.Error(w, "SPA build not found at "+index+". Run the frontend build and restart wacalls.", http.StatusNotFound)
			return
		}

		fs := http.FileServer(http.Dir(dir))
		full := filepath.Join(dir, filepath.Clean(p))
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}
		// Fallback to index.html for SPA routes.
		http.ServeFile(w, r, index)
	})
}

func withCORS(h http.Handler) http.Handler {
	// Allowlist de origens cross-site. Vazio (default) = mesma origem apenas,
	// nenhum cabeçalho CORS é emitido para origens externas — bloqueando que
	// sites maliciosos façam requisições autenticadas usando o cookie do
	// usuário. Configure WACALLS_ALLOWED_ORIGINS="https://app.exemplo.com,https://outro.com"
	// quando precisar liberar um domínio adicional.
	raw := strings.TrimSpace(os.Getenv("WACALLS_ALLOWED_ORIGINS"))
	allowed := map[string]bool{}
	allowAll := false
	for _, o := range strings.Split(raw, ",") {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		if o == "*" {
			allowAll = true
			continue
		}
		allowed[strings.ToLower(o)] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		// "Vary: Origin" precisa ser emitido sempre que a resposta pode
		// variar com a origem — inclusive nas same-origin para evitar
		// envenenamento de cache.
		w.Header().Add("Vary", "Origin")
		if origin != "" {
			lower := strings.ToLower(origin)
			switch {
			case allowAll:
				// Curinga: sem credenciais (regra do navegador).
				w.Header().Set("Access-Control-Allow-Origin", "*")
			case allowed[lower]:
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			default:
				// Origem desconhecida: não emitimos cabeçalhos CORS, o
				// navegador bloqueia a requisição autenticada cross-site.
			}
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Client-Id, Authorization")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// withSecurityHeaders adiciona cabeçalhos de defesa em profundidade em todas
// as respostas (HTML, JSON, mídia). Mantemos a política conservadora: bloqueia
// embedding em iframes (clickjacking), nega sniffing de Content-Type e limita
// o vazamento de Referer para origens externas.
func withSecurityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(self), camera=(self)")
		// HSTS só faz sentido quando a conexão é realmente HTTPS — senão
		// quebraria desenvolvimento local em http://localhost.
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		// Evita que crawlers indexem endpoints internos quando expostos.
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		}
		h.ServeHTTP(w, r)
	})
}

// safeFileServer wraps http.FileServer to reject path traversal attempts
// (e.g. encoded "..") that could escape the configured directory.
func safeFileServer(root http.FileSystem) http.Handler {
	fs := http.FileServer(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := filepath.Clean("/" + r.URL.Path)
		if strings.Contains(clean, "..") {
			http.NotFound(w, r)
			return
		}
		fs.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func clientID(r *http.Request) string {
	if id := r.Header.Get("X-Client-Id"); id != "" {
		return id
	}
	return r.URL.Query().Get("clientId")
}

// sessionByID is the auth-aware variant used by every authenticated route.
// It returns 404 when the session does not exist OR when the caller does not
// own it (admins bypass this check). 403 would leak existence, so we mirror
// 404 either way.
func (s *server) sessionByID(w http.ResponseWriter, r *http.Request, sid string) *Session {
	sess, ok := s.sessions.Get(sid)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such session"})
		return nil
	}
	u := currentUserFromReq(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return nil
	}
	if !u.IsSuperAdmin() && sess.ownerID != u.ID {
		if s.sessions.tenantOf(sid) == "" || s.sessions.tenantOf(sid) != u.TenantID() {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such session"})
			return nil
		}
	}
	if !u.IsSuperAdmin() && sess.ownerID == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such session"})
		return nil
	}
	return sess
}

func (s *server) handleEvents(w http.ResponseWriter, r *http.Request) {
	u := s.resolveUser(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	s.broker.serveSSE(w, r, u.ID, u.ID, u.TenantID(), u.IsSuperAdmin())
}

func (s *server) handleSessionList(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	writeJSON(w, http.StatusOK, map[string]any{"sessions": s.sessions.infosFor(u.ID, u.IsSuperAdmin())})
}

func (s *server) handleSessionCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		Plan string `json:"plan"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = "Session"
	}
	u := currentUserFromReq(r)
	plan := strings.ToLower(strings.TrimSpace(body.Plan))
	// Plano gratuito: 1 conexão por usuário. Conexões pagas podem ser criadas
	// como pendentes para permitir abrir o checkout; o pareamento cobra depois.
	if plan != "paid" {
		if qerr := s.enforceFreeTier(r.Context(), u.ID, u.IsAdmin(), "free_connections", len(s.sessions.infosFor(u.ID, false))); qerr != nil {
			writeQuotaError(w, qerr)
			return
		}
	}
	if qerr := s.enforceQuota(r.Context(), "conexoes", len(s.sessions.infosFor(u.ID, false))); qerr != nil {
		writeQuotaError(w, qerr)
		return
	}
	id, err := s.sessions.Create(name, u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Conexões não passam por bumpWeeklyUsage; verifica limiares aqui.
	s.maybeNotifyFreeTierThreshold(r.Context(), u.ID, "free_connections")
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

func (s *server) handleSessionUpdate(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	var body struct {
		Name              string `json:"name"`
		Color             string `json:"color"`
		IsDefault         bool   `json:"isDefault"`
		AllowGroups       bool   `json:"allowGroups"`
		QueueID           string `json:"queueId"`
		RedirectMinutes   int    `json:"redirectMinutes"`
		FlowID            string `json:"flowId"`
		ChatFlowID        string `json:"chatFlowId"`
		GreetingMessage   string `json:"greetingMessage"`
		CompletionMessage string `json:"completionMessage"`
		OutOfHoursMessage string `json:"outOfHoursMessage"`
		SurveyEnabled     bool   `json:"surveyEnabled"`
		SurveyPrompt      string `json:"surveyPrompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = sess.name
	}
	if body.Color == "" {
		body.Color = "#57adf8"
	}
	voiceFlowID := strings.TrimSpace(body.FlowID)
	chatFlowID := strings.TrimSpace(body.ChatFlowID)
	// Se há chatbot vinculado, não mantém URA escondida no flow_id. Chamada sem
	// URA de voz explícita deve tocar para o atendente atender manualmente.
	if chatFlowID != "" {
		voiceFlowID = ""
	}
	// Backward compatibility: older frontends saved the selected chatbot into
	// flowId. Split it here so calls do not auto-accept a chat-only flow and
	// messages still trigger the chatbot linked to the connection.
	if voiceFlowID != "" && s.flows != nil {
		if f, ferr := s.flows.Get(r.Context(), voiceFlowID); ferr == nil && f != nil && flowKind(f) == "chat" {
			if chatFlowID == "" {
				chatFlowID = voiceFlowID
			}
			voiceFlowID = ""
		}
	}
	// Pre-check: vincular um fluxo desabilitado é a causa #1 de URA/chatbot não
	// disparar. Falhamos cedo com mensagem clara em vez de aceitar silenciosamente.
	if fid := voiceFlowID; fid != "" && s.flows != nil {
		f, ferr := s.flows.Get(r.Context(), fid)
		if ferr != nil || f == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "fluxo informado não existe"})
			return
		}
		if flowKind(f) == "chat" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "selecione um fluxo de voz (URA) no campo de chamadas"})
			return
		}
		if !f.Enabled {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "habilite o fluxo \"" + f.Name + "\" no FlowBuilder antes de vinculá-lo à conexão"})
			return
		}
	}
	if fid := chatFlowID; fid != "" && s.flows != nil {
		f, ferr := s.flows.Get(r.Context(), fid)
		if ferr != nil || f == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "fluxo de conversa informado não existe"})
			return
		}
		if flowKind(f) != "chat" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "selecione um fluxo de conversa (chatbot) no campo de mensagens"})
			return
		}
		if !f.Enabled {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "habilite o fluxo \"" + f.Name + "\" no FlowBuilder antes de vinculá-lo à conexão"})
			return
		}
	}
	if err := s.sessions.Update(r.Context(), sess.id, sessionUpdate{
		Name: name, Color: body.Color, IsDefault: body.IsDefault, AllowGroups: body.AllowGroups,
		QueueID: body.QueueID, RedirectMinutes: body.RedirectMinutes, FlowID: voiceFlowID, ChatFlowID: chatFlowID,
		GreetingMessage:   body.GreetingMessage,
		CompletionMessage: body.CompletionMessage,
		OutOfHoursMessage: body.OutOfHoursMessage,
		SurveyEnabled:     body.SurveyEnabled,
		SurveyPrompt:      body.SurveyPrompt,
	}); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleSessionRegenToken(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	tok, err := s.sessStore.regenerateToken(r.Context(), sess.id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	sess.mu.Lock()
	sess.integrationToken = tok
	sess.mu.Unlock()
	s.broker.emitSessionList(s.sessions.infos())
	writeJSON(w, http.StatusOK, map[string]string{"token": tok})
}

func (s *server) handleSessionDelete(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r, r.PathValue("sid")); sess == nil {
		return
	}
	if err := s.sessions.Delete(r.Context(), r.PathValue("sid")); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleSessionLogout(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r, r.PathValue("sid")); sess == nil {
		return
	}
	if err := s.sessions.Logout(r.Context(), r.PathValue("sid")); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleSessionPair(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	u := currentUserFromReq(r)
	if u != nil {
		if qerr := s.enforcePaidConnect(r.Context(), u.ID, u.IsAdmin()); qerr != nil {
			writeQuotaError(w, qerr)
			return
		}
	}
	if err := s.sessions.Pair(r.PathValue("sid")); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleStartCall(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r, r.PathValue("sid")); sess != nil {
		s.doStartCall(sess, w, r)
	}
}

func (s *server) handleWebRTC(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r, r.PathValue("sid")); sess != nil {
		s.doWebRTC(sess, w, r)
	}
}

func (s *server) handleAccept(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r, r.PathValue("sid")); sess != nil {
		s.doAccept(sess, w, r)
	}
}

func (s *server) handleReject(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r, r.PathValue("sid")); sess != nil {
		s.doReject(sess, w, r)
	}
}

func (s *server) handleEndCall(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r, r.PathValue("sid")); sess != nil {
		s.doEndCall(sess, w, r)
	}
}

func (s *server) handleHistory(w http.ResponseWriter, r *http.Request) {
	if sess := s.sessionByID(w, r, r.PathValue("sid")); sess != nil {
		rows := s.broker.historyRows(sess.id, 50)
		// Augment each row with recording metadata, if present.
		type historyOut struct {
			CallRecord
			Recording *RecordingInfo `json:"recording,omitempty"`
		}
		out := make([]historyOut, 0, len(rows))
		for _, r0 := range rows {
			item := historyOut{CallRecord: r0}
			if info, ok, err := s.calls.RecordingByCall(r.Context(), r0.CallID); err == nil && ok {
				cp := info
				item.Recording = &cp
			}
			out = append(out, item)
		}
		writeJSON(w, http.StatusOK, map[string]any{"rows": out})
	}
}

func (s *server) doStartCall(sess *Session, w http.ResponseWriter, r *http.Request) {
	if sess.client.Store.ID == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not paired"})
		return
	}
	// Free tier: 50 chamadas/semana por usuário. Plano pago = ilimitado.
	u := currentUserFromReq(r)
	if u != nil {
		if qerr := s.enforceFreeTier(r.Context(), u.ID, u.IsAdmin(), "free_calls", s.weeklyUsage(r.Context(), u.ID, "free_calls")); qerr != nil {
			writeQuotaError(w, qerr)
			return
		}
	}
	var body struct {
		Phone      string `json:"phone"`
		DurationMs int    `json:"duration_ms"`
		Record     bool   `json:"record"`
		Video      bool   `json:"video"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Phone) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "phone required"})
		return
	}
	owner := clientID(r)
	if other := s.broker.ownerActiveCall(owner); other != "" {
		// Self-heal: if the call manager no longer tracks this call (e.g. the
		// previous dial died right after sending the offer and we never got an
		// OnEnded), drop the stale broker entry so the operator can dial again.
		stale := true
		if rec, found := s.broker.getCall(other); found {
			if ac, ok := sess.reg.get(other); ok && ac != nil && rec.Status != StatusEnded {
				stale = false
			}
		}
		if stale {
			s.broker.endCall(other, "stale")
		} else {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "operator already on a call"})
			return
		}
	}
	if max := s.sessions.maxCalls; max > 0 && sess.reg.count() >= max {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "max concurrent calls"})
		return
	}
	peer, err := parseDialJID(body.Phone)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	callID, err := sess.startOutgoing(r.Context(), peer, body.Video)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if u != nil {
		s.bumpWeeklyUsage(r.Context(), u.ID, "free_calls")
	}
	// Prefer the resolved (LID) peer for the broker/UI so operators can see
	// at a glance whether LID resolution succeeded.
	actualPeer := peer.String()
	if ac, ok := sess.reg.get(callID); ok && ac != nil {
		if ci := ac.cm.CurrentCall(); ci != nil && ci.PeerJid != "" {
			actualPeer = ci.PeerJid
		}
	}
	s.broker.upsertCall(CallRecord{
		SessionID: sess.id, CallID: callID, Owner: &owner, Direction: "outbound", Peer: actualPeer,
		StartedAt: time.Now().UnixMilli(), Status: StatusRinging,
	})
	writeJSON(w, http.StatusOK, map[string]any{"call": map[string]string{"callId": callID}})
}

func (s *server) doWebRTC(sess *Session, w http.ResponseWriter, r *http.Request) {
	callID := r.PathValue("id")
	ac, ok := sess.reg.get(callID)
	if !ok {
		// The CallManager removed the entry — most often because WhatsApp
		// returned a terminate stanza right after our offer (recipient is
		// unreachable, blocked the caller, or the JID/LID is not a real
		// user). Surface the actual reason so the operator knows why the
		// recipient's phone never rang instead of a confusing 404.
		msg := "a chamada foi encerrada antes de iniciar"
		if rec, found := s.broker.getCall(callID); found && rec.EndReason != "" {
			msg = "chamada encerrada (" + rec.EndReason + ")"
		}
		// Release the operator slot so the next dial attempt isn't blocked
		// by a stale "operator already on a call" 409.
		s.broker.endCall(callID, "aborted-before-sdp")
		writeJSON(w, http.StatusNotFound, map[string]string{"error": msg})
		return
	}
	var body struct {
		SDPOffer string `json:"sdp_offer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SDPOffer == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sdp_offer required"})
		return
	}
	bridge, answer, err := NewBridge(body.SDPOffer, s.log)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	bridge.OnBrowserPCM = func(pcm []float32) {
		ac.cm.FeedCapturedPCM(pcm)
	}
	bridge.OnBrowserVideo = func(au []byte) {
		ac.cm.FeedCapturedVideo(au)
	}
	bridge.OnTerminalICE = func() {
		go sess.terminateCall(callID, core.EndCallReasonUserEnded)
	}
	sess.setBridge(callID, bridge)
	writeJSON(w, http.StatusOK, map[string]string{"sdp_answer": answer})
}

func (s *server) doAccept(sess *Session, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ac, ok := sess.reg.get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
		return
	}
	owner := clientID(r)
	if other := s.broker.ownerActiveCall(owner); other != "" && other != id {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "operator already on a call"})
		return
	}
	if !s.broker.setOwner(id, owner) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "claimed by another client"})
		return
	}
	s.broker.emitIncomingClaimed(sess.id, id, owner)
	if err := ac.cm.AcceptCall(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"call": map[string]string{"callId": id}})
}

func (s *server) doReject(sess *Session, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if ac, ok := sess.reg.get(id); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = ac.cm.RejectCall(ctx, id, core.EndCallReasonDeclined)
	}
	sess.removeCall(id)
	s.broker.endCall(id, string(core.EndCallReasonDeclined))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) doEndCall(sess *Session, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if ac, ok := sess.reg.get(id); ok {
		_ = ac.cm.EndCall(r.Context(), core.EndCallReasonUserEnded)
	}
	sess.removeCall(id)
	s.broker.endCall(id, string(core.EndCallReasonUserEnded))
	w.WriteHeader(http.StatusNoContent)
}

func normalizePhone(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "+")
	var b strings.Builder
	for _, c := range p {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// parseDialJID accepts user input like "5511999999999", "+55 11 9...",
// "5511999999999@s.whatsapp.net", or "29193630920911@lid" and returns the
// proper JID. When the input carries an explicit @lid suffix we MUST keep
// the LID server — many WhatsApp contacts only expose a LID (no phone
// number), and sending a call offer to a fabricated PN JID is silently
// dropped by the server, so the recipient's phone never rings.
func parseDialJID(raw string) (types.JID, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return types.JID{}, fmt.Errorf("phone required")
	}
	if strings.Contains(s, "@") {
		jid, err := types.ParseJID(s)
		if err != nil {
			return types.JID{}, fmt.Errorf("invalid jid: %w", err)
		}
		if jid.User == "" {
			return types.JID{}, fmt.Errorf("invalid jid: empty user")
		}
		return jid, nil
	}
	digits := normalizePhone(s)
	if digits == "" {
		return types.JID{}, fmt.Errorf("phone required")
	}
	return types.NewJID(digits, types.DefaultUserServer), nil
}

// Maximum upload size for a single call recording (200 MB). Anything larger
// almost always indicates a buggy MediaRecorder; rejecting up-front protects
// disk usage and SQLite metadata.
const maxRecordingBytes int64 = 200 * 1024 * 1024

func recordingsDir() string { return filepath.Join("media", "recordings") }

func extForMime(m string) string {
	switch {
	case strings.HasPrefix(m, "video/webm"):
		return ".webm"
	case strings.HasPrefix(m, "video/mp4"):
		return ".mp4"
	case strings.HasPrefix(m, "audio/webm"):
		return ".webm"
	case strings.HasPrefix(m, "audio/ogg"):
		return ".ogg"
	case strings.HasPrefix(m, "audio/mp4"), strings.HasPrefix(m, "audio/aac"):
		return ".m4a"
	case strings.HasPrefix(m, "audio/mpeg"):
		return ".mp3"
	case strings.HasPrefix(m, "audio/wav"), strings.HasPrefix(m, "audio/x-wav"):
		return ".wav"
	}
	return ".bin"
}

func newShareToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *server) handleUploadRecording(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	callID := r.PathValue("id")
	if strings.TrimSpace(callID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "call id required"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRecordingBytes+1024)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid upload: " + err.Error()})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing 'file' field"})
		return
	}
	defer file.Close()

	mime := header.Header.Get("Content-Type")
	if mime == "" {
		mime = r.FormValue("mime")
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	if !(strings.HasPrefix(mime, "audio/") || strings.HasPrefix(mime, "video/")) {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "only audio/video uploads allowed"})
		return
	}

	if err := os.MkdirAll(recordingsDir(), 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot create recordings dir"})
		return
	}
	// Use the callID as the filename to make replacement idempotent.
	safeID := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, callID)
	ext := extForMime(mime)
	relPath := filepath.Join("recordings", safeID+ext)
	absPath := filepath.Join("media", relPath)

	dst, err := os.Create(absPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot write file"})
		return
	}
	written, copyErr := io.Copy(dst, file)
	_ = dst.Close()
	if copyErr != nil {
		_ = os.Remove(absPath)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "upload failed: " + copyErr.Error()})
		return
	}
	if written > maxRecordingBytes {
		_ = os.Remove(absPath)
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "file too large"})
		return
	}

	// Reuse a previous share token if the recording is being replaced so old
	// share links keep working.
	token := ""
	if prev, ok, _ := s.calls.RecordingByCall(r.Context(), callID); ok && prev.Token != "" {
		token = prev.Token
		if prev.Path != "" && prev.Path != relPath {
			_ = os.Remove(filepath.Join("media", prev.Path))
		}
	}
	if token == "" {
		t, err := newShareToken()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token gen failed"})
			return
		}
		token = t
	}

	info := RecordingInfo{
		CallID: callID, Path: relPath, Mime: mime, Size: written, Token: token,
		Uploaded: time.Now().UnixMilli(),
	}
	if err := s.calls.SetRecording(r.Context(), callID, info); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "persist failed: " + err.Error()})
		return
	}
	// Mint a short-lived signed URL so the caller can immediately play/share.
	sig, exp := s.recSigner.Sign(info.Token, 10*time.Minute, false)
	dlSig, _ := s.recSigner.Sign(info.Token, 10*time.Minute, true)
	base := "/api/recordings/" + info.Token
	writeJSON(w, http.StatusOK, map[string]any{
		"callId":      info.CallID,
		"mime":        info.Mime,
		"size":        info.Size,
		"token":       info.Token,
		"shareUrl":    fmt.Sprintf("%s?exp=%d&sig=%s", base, exp, sig),
		"downloadUrl": fmt.Sprintf("%s?exp=%d&sig=%s&download=1", base, exp, dlSig),
		"expiresAt":   exp * 1000,
		"uploadedAt":  info.Uploaded,
	})
}

// handleDownloadRecording serves the file referenced by a share token.
// Access requires either a valid signed URL (exp+sig query params) or an
// authenticated session cookie belonging to the owner of the call (or admin).
func (s *server) handleDownloadRecording(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if strings.TrimSpace(token) == "" {
		http.NotFound(w, r)
		return
	}
	info, ok, err := s.calls.RecordingByToken(r.Context(), token)
	if err != nil || !ok {
		http.NotFound(w, r)
		return
	}
	download := r.URL.Query().Get("download") == "1"
	if !s.recordingAccessAllowed(r, info, download) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	abs := filepath.Join("media", info.Path)
	f, err := os.Open(abs)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	stat, _ := f.Stat()
	if info.Mime != "" {
		w.Header().Set("Content-Type", info.Mime)
	}
	if download {
		w.Header().Set("Content-Disposition",
			fmt.Sprintf(`attachment; filename="chamada-%s%s"`, info.CallID, extForMime(info.Mime)))
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, filepath.Base(info.Path), stat.ModTime(), f)
}

// recordingAccessAllowed accepts a signed URL OR an authenticated owner/admin.
func (s *server) recordingAccessAllowed(r *http.Request, info RecordingInfo, download bool) bool {
	q := r.URL.Query()
	if sig := q.Get("sig"); sig != "" {
		if err := s.recSigner.Verify(info.Token, sig, parseExp(q.Get("exp")), download); err == nil {
			return true
		}
	}
	u := s.resolveUser(r)
	if u == nil {
		return false
	}
	if u.IsAdmin() {
		return true
	}
	_, owner, ok, _ := s.calls.CallMeta(r.Context(), info.CallID)
	return ok && owner != "" && owner == u.ID
}

// handleSignRecording mints a short-lived signed URL for a recording.
// The caller must own the session and the call.
func (s *server) handleSignRecording(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	callID := r.PathValue("id")
	info, ok, err := s.calls.RecordingByCall(r.Context(), callID)
	if err != nil || !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no recording"})
		return
	}
	// Confirm the call really belongs to this session (defense in depth).
	if sid, _, ok, _ := s.calls.CallMeta(r.Context(), callID); !ok || sid != sess.id {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	ttl := 10 * time.Minute
	if v := r.URL.Query().Get("ttl"); v != "" {
		if n, err := time.ParseDuration(v + "s"); err == nil && n > 0 && n <= 24*time.Hour {
			ttl = n
		}
	}
	sig, exp := s.recSigner.Sign(info.Token, ttl, false)
	dlSig, _ := s.recSigner.Sign(info.Token, ttl, true)
	base := "/api/recordings/" + info.Token
	writeJSON(w, http.StatusOK, map[string]any{
		"token":       info.Token,
		"mime":        info.Mime,
		"size":        info.Size,
		"shareUrl":    fmt.Sprintf("%s?exp=%d&sig=%s", base, exp, sig),
		"downloadUrl": fmt.Sprintf("%s?exp=%d&sig=%s&download=1", base, exp, dlSig),
		"expiresAt":   exp * 1000,
	})
}
