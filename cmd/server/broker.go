package main

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type CallStatus string

const (
	StatusStarting  CallStatus = "starting"
	StatusRinging   CallStatus = "ringing"
	StatusConnected CallStatus = "connected"
	StatusEnded     CallStatus = "ended"
)

type CallRecord struct {
	SessionID string     `json:"sessionId"`
	CallID    string     `json:"callId"`
	Owner     *string    `json:"owner"`
	Direction string     `json:"direction"`
	Peer      string     `json:"peer"`
	StartedAt int64      `json:"startedAt"`
	Status    CallStatus `json:"status"`
	EndedAt   *int64     `json:"endedAt,omitempty"`
	EndReason string     `json:"endReason,omitempty"`
	Answered  bool       `json:"answered,omitempty"`
}

type AuthSnapshot struct {
	State  string `json:"state"`
	Paired bool   `json:"paired"`
	QR     string `json:"qr,omitempty"`
}

type SessionInfo struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	JID               string `json:"jid"`
	State             string `json:"state"`
	Paired            bool   `json:"paired"`
	Mode              string `json:"mode,omitempty"`
	CloudPhoneID      string `json:"cloudPhoneId,omitempty"`
	CloudWABAID       string `json:"cloudWabaId,omitempty"`
	CloudConfigured   bool   `json:"cloudConfigured,omitempty"`
	OwnerID           string `json:"ownerId,omitempty"`
	AvatarURL         string `json:"avatarUrl,omitempty"`
	Color             string `json:"color,omitempty"`
	IsDefault         bool   `json:"isDefault,omitempty"`
	AllowGroups       bool   `json:"allowGroups,omitempty"`
	IntegrationToken  string `json:"integrationToken,omitempty"`
	QueueID           string `json:"queueId,omitempty"`
	RedirectMinutes   int    `json:"redirectMinutes,omitempty"`
	FlowID            string `json:"flowId,omitempty"`
	ChatFlowID        string `json:"chatFlowId,omitempty"`
	GreetingMessage   string `json:"greetingMessage,omitempty"`
	CompletionMessage string `json:"completionMessage,omitempty"`
	OutOfHoursMessage string `json:"outOfHoursMessage,omitempty"`
	SurveyEnabled     bool   `json:"surveyEnabled,omitempty"`
	SurveyPrompt      string `json:"surveyPrompt,omitempty"`
}

type subscriber struct {
	clientID string
	userID   string
	tenantID string
	isAdmin  bool
	ch       chan []byte
}

type Broker struct {
	mu      sync.RWMutex
	subs    map[*subscriber]struct{}
	calls   map[string]*CallRecord
	history []CallRecord

	SnapshotFn    func(userID string, isAdmin bool) []any
	SessionOwner  func(sessionID string) string
	SessionTenant func(sessionID string) string
	SessionsForFn func(userID string, isAdmin bool) []SessionInfo
	PersistCall   func(rec CallRecord)
}

func NewBroker() *Broker {
	return &Broker{
		subs:  map[*subscriber]struct{}{},
		calls: map[string]*CallRecord{},
	}
}

func (b *Broker) subscribe(clientID, userID, tenantID string, isAdmin bool) *subscriber {
	s := &subscriber{clientID: clientID, userID: userID, tenantID: tenantID, isAdmin: isAdmin, ch: make(chan []byte, 32)}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s
}

func (b *Broker) unsubscribe(s *subscriber) {
	b.mu.Lock()
	delete(b.subs, s)
	b.mu.Unlock()
	close(s.ch)
}

func (b *Broker) broadcast(ev any) {
	b.deliverScoped("", ev)
}

// deliverScoped sends `ev` to all subscribers, optionally restricted to admins
// and the owner of the given session (when sessionID is non-empty and we know
// who owns it).
func (b *Broker) deliverScoped(sessionID string, ev any) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	owner := ""
	tenant := ""
	scoped := sessionID != ""
	if scoped && b.SessionOwner != nil {
		owner = b.SessionOwner(sessionID)
	}
	if scoped && b.SessionTenant != nil {
		tenant = b.SessionTenant(sessionID)
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for s := range b.subs {
		// Eventos com escopo por sessão NUNCA podem vazar para outros tenants.
		// Se o dono não foi resolvido (sessão antiga sem owner_id), só admins
		// recebem — clientes não-admin ficam sem aquele evento por segurança.
		if scoped && !s.isAdmin {
			allowed := owner != "" && s.userID == owner
			if !allowed && tenant != "" && s.tenantID != "" {
				allowed = s.tenantID == tenant
			}
			if !allowed {
				continue
			}
		}
		select {
		case s.ch <- data:
		default:
		}
	}
}

func (b *Broker) emitAuthState(sessionID string, a AuthSnapshot) {
	b.deliverScoped(sessionID, map[string]any{
		"type": "auth-state", "sessionId": sessionID,
		"paired": a.Paired, "state": a.State, "qr": a.QR,
	})
}

// emitSessionList sends a per-user filtered session list when a filter
// function is configured. Tests that don't set SessionsForFn fall back to the
// raw broadcast of the provided slice.
func (b *Broker) emitSessionList(sessions []SessionInfo) {
	if b.SessionsForFn == nil {
		b.broadcast(map[string]any{"type": "session-list", "sessions": sessions})
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for s := range b.subs {
		list := b.SessionsForFn(s.userID, s.isAdmin)
		data, _ := json.Marshal(map[string]any{"type": "session-list", "sessions": list})
		select {
		case s.ch <- data:
		default:
		}
	}
}

func (b *Broker) emitSessionQR(sessionID, qr string) {
	b.deliverScoped(sessionID, map[string]any{"type": "session-qr", "sessionId": sessionID, "qr": qr})
}

// emitToUser delivers an event to all subscribers of the given userID (and to
// admins). Used for personal channels like billing updates so the user's
// browser reacts in real time without polling.
func (b *Broker) emitToUser(userID string, ev any) {
	if userID == "" {
		return
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for s := range b.subs {
		if s.userID != userID && !s.isAdmin {
			continue
		}
		select {
		case s.ch <- data:
		default:
		}
	}
}

// emitBilling publishes a subscription update (status Pago/Pendente) to the
// specific user. The frontend BillingPage listens for `type=billing-update`
// and refreshes its view without waiting for polling.
func (b *Broker) emitBilling(userID, status, planID string, currentPeriodEnd int64) {
	b.emitToUser(userID, map[string]any{
		"type":             "billing-update",
		"userId":           userID,
		"status":           status,
		"planId":           planID,
		"currentPeriodEnd": currentPeriodEnd,
		"ts":               time.Now().UnixMilli(),
	})
}

func (b *Broker) upsertCall(r CallRecord) {
	b.mu.Lock()
	cp := r
	if prev, ok := b.calls[r.CallID]; ok && prev.Answered {
		cp.Answered = true
	}
	if cp.Status == StatusConnected {
		cp.Answered = true
	}
	b.calls[r.CallID] = &cp
	b.mu.Unlock()
	b.broadcastCallList()
	b.deliverScoped(r.SessionID, map[string]any{
		"type": "call-status", "sessionId": r.SessionID, "id": r.CallID, "owner": r.Owner,
		"status": r.Status, "peer": r.Peer, "startedAt": r.StartedAt,
	})
}

func (b *Broker) getCall(id string) (*CallRecord, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	c, ok := b.calls[id]
	if !ok {
		return nil, false
	}
	cp := *c
	return &cp, true
}

func (b *Broker) setOwner(id, owner string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.calls[id]
	if !ok {
		return false
	}
	if c.Owner != nil && *c.Owner != owner {
		return false
	}
	c.Owner = &owner
	return true
}

func (b *Broker) ownerActiveCall(owner string) string {
	if owner == "" {
		return ""
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for id, c := range b.calls {
		if c.Owner != nil && *c.Owner == owner && c.Status != StatusEnded {
			return id
		}
	}
	return ""
}

func (b *Broker) endCall(id, reason string) {
	b.mu.Lock()
	c, ok := b.calls[id]
	if !ok {
		b.mu.Unlock()
		return
	}
	now := time.Now().UnixMilli()
	c.Status = StatusEnded
	c.EndedAt = &now
	c.EndReason = reason
	ended := *c
	delete(b.calls, id)
	b.history = append(b.history, ended)
	owner := c.Owner
	sessionID := c.SessionID
	b.mu.Unlock()

	if b.PersistCall != nil {
		b.PersistCall(ended)
	}

	b.deliverScoped(sessionID, map[string]any{
		"type": "call-ended", "sessionId": sessionID, "id": id, "owner": owner, "reason": reason, "endedAt": now,
	})
	b.broadcastCallList()
}

func (b *Broker) broadcastCallList() {
	b.mu.RLock()
	all := make([]CallRecord, 0, len(b.calls))
	for _, c := range b.calls {
		all = append(all, *c)
	}
	subs := make([]*subscriber, 0, len(b.subs))
	for s := range b.subs {
		subs = append(subs, s)
	}
	b.mu.RUnlock()

	// Group calls by session and resolve owners once.
	if b.SessionOwner == nil {
		data, _ := json.Marshal(map[string]any{"type": "call-list", "calls": all})
		b.mu.RLock()
		defer b.mu.RUnlock()
		for s := range b.subs {
			select {
			case s.ch <- data:
			default:
			}
		}
		return
	}
	for _, s := range subs {
		filtered := all[:0:0]
		for _, c := range all {
			owner := b.SessionOwner(c.SessionID)
			tenant := ""
			if b.SessionTenant != nil {
				tenant = b.SessionTenant(c.SessionID)
			}
			if s.isAdmin || (owner != "" && s.userID == owner) || (tenant != "" && s.tenantID != "" && tenant == s.tenantID) {
				filtered = append(filtered, c)
			}
		}
		data, _ := json.Marshal(map[string]any{"type": "call-list", "calls": filtered})
		select {
		case s.ch <- data:
		default:
		}
	}
}

func (b *Broker) emitIncoming(sessionID, id, peer, peerName string, video bool) {
	b.deliverScoped(sessionID, map[string]any{
		"type": "incoming", "sessionId": sessionID, "id": id, "peer": peer,
		"peerName": peerName,
		"video":    video, "offeredAt": time.Now().UnixMilli(),
	})
}

func (b *Broker) emitIncomingClaimed(sessionID, id, owner string) {
	b.deliverScoped(sessionID, map[string]any{"type": "incoming-claimed", "sessionId": sessionID, "id": id, "owner": owner})
}

// emitUraAutoAttend sinaliza ao frontend que a URA assumiu o atendimento
// automaticamente — usado para exibir um aviso (toast) ao operador, já
// que o modal "Incoming call" é suprimido nesse caso.
func (b *Broker) emitUraAutoAttend(sessionID, id, peer, peerName string, video bool, ts int64) {
	b.deliverScoped(sessionID, map[string]any{
		"type":      "ura-auto-attend",
		"sessionId": sessionID,
		"id":        id,
		"peer":      peer,
		"peerName":  peerName,
		"video":     video,
		"ts":        ts,
	})
}

func (b *Broker) emitMessage(m MessageRow) {
	b.deliverScoped(m.SessionID, map[string]any{
		"type":      "message",
		"sessionId": m.SessionID,
		"chatJid":   m.ChatJID,
		"message":   m,
	})
}

func (b *Broker) emitChatMeta(m ChatMeta) {
	b.deliverScoped(m.SessionID, map[string]any{
		"type": "chat-meta",
		"meta": m,
	})
}

func (b *Broker) emitChatEvent(e ChatEvent) {
	b.deliverScoped(e.SessionID, map[string]any{
		"type":  "chat-event",
		"event": e,
	})
}

func (b *Broker) historyRows(sessionID string, limit int) []CallRecord {
	b.mu.RLock()
	defer b.mu.RUnlock()
	rows := make([]CallRecord, 0, limit)
	for i := len(b.history) - 1; i >= 0 && len(rows) < limit; i-- {
		if sessionID == "" || b.history[i].SessionID == sessionID {
			rows = append(rows, b.history[i])
		}
	}
	return rows
}

func (b *Broker) serveSSE(w http.ResponseWriter, r *http.Request, clientID, userID, tenantID string, isAdmin bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	sub := b.subscribe(clientID, userID, tenantID, isAdmin)
	defer b.unsubscribe(sub)

	if b.SnapshotFn != nil {
		for _, ev := range b.SnapshotFn(userID, isAdmin) {
			writeSSE(w, flusher, ev)
		}
	}
	b.broadcastCallList()

	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case data := <-sub.ch:
			if _, err := w.Write(append(append([]byte("data: "), data...), '\n', '\n')); err != nil {
				return
			}
			flusher.Flush()
		case <-keepalive.C:
			w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, f http.Flusher, ev any) {
	data, _ := json.Marshal(ev)
	w.Write(append(append([]byte("data: "), data...), '\n', '\n'))
	f.Flush()
}
