package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type SessionManager struct {
	appCtx    context.Context
	container *sqlstore.Container
	broker    *Broker
	store     *sessionStore
	waLogger  waLog.Logger
	log       *slog.Logger
	maxCalls  int

	flowExec *FlowExecutor
	messages *messageStore
	chatMeta *chatMetaStore
	kanban   *kanbanStore
	calls    *callStore
	// UserTenantFn resolves a user/operator to the tenant root company.
	// It lets the connection list stay shared inside one company while never
	// exposing sessions from another company.
	UserTenantFn func(userID string) string
	// IsAdminRoleFn returns true when the user has the tenant "admin" role.
	// Tenant admins see every connection created inside their company;
	// non-admin operators only see the connections explicitly linked to them.
	IsAdminRoleFn func(userID string) bool
	// UserSessionsFn returns the session IDs (connections) explicitly
	// assigned to a user via the "Usuários" screen. Used to filter what
	// non-admin operators can see.
	UserSessionsFn func(userID string) []string

	mu       sync.RWMutex
	sessions map[string]*Session
	order    []string
}

func newSessionManager(ctx context.Context, container *sqlstore.Container, broker *Broker, store *sessionStore, waLogger waLog.Logger, log *slog.Logger, maxCalls int) *SessionManager {
	return &SessionManager{
		appCtx:    ctx,
		container: container,
		broker:    broker,
		store:     store,
		waLogger:  waLogger,
		log:       log,
		maxCalls:  maxCalls,
		sessions:  map[string]*Session{},
	}
}

func (m *SessionManager) register(s *Session) {
	m.mu.Lock()
	m.sessions[s.id] = s
	m.order = append(m.order, s.id)
	m.mu.Unlock()
}

func (m *SessionManager) unregister(id string) {
	m.mu.Lock()
	delete(m.sessions, id)
	for i, x := range m.order {
		if x == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
}

func (m *SessionManager) Get(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

func (m *SessionManager) infos() []SessionInfo {
	m.mu.RLock()
	ordered := make([]*Session, 0, len(m.order))
	for _, id := range m.order {
		if s, ok := m.sessions[id]; ok {
			ordered = append(ordered, s)
		}
	}
	m.mu.RUnlock()
	out := make([]SessionInfo, 0, len(ordered))
	for _, s := range ordered {
		out = append(out, s.info())
	}
	return out
}

// infosFor returns the sessions visible to a given user. The SaaS super-admin
// sees every session. Tenant admins see every session inside their company
// (owner or operators under the same parent_id). Regular operators only see
// the connections explicitly linked to them via user_sessions_link, plus any
// they own themselves.
func (m *SessionManager) infosFor(userID string, isAdmin bool) []SessionInfo {
	all := m.infos()
	if isAdmin {
		return all
	}
	tenantID := userID
	if m.UserTenantFn != nil {
		if t := m.UserTenantFn(userID); t != "" {
			tenantID = t
		}
	}
	// Tenant admin -> full company view.
	isTenantAdmin := false
	if m.IsAdminRoleFn != nil {
		isTenantAdmin = m.IsAdminRoleFn(userID)
	}
	// Sub-user -> intersect with explicit links.
	var linked map[string]struct{}
	if !isTenantAdmin && m.UserSessionsFn != nil {
		ids := m.UserSessionsFn(userID)
		linked = make(map[string]struct{}, len(ids))
		for _, id := range ids {
			linked[id] = struct{}{}
		}
	}
	out := make([]SessionInfo, 0, len(all))
	for _, s := range all {
		// Tenant boundary first: never leak across companies.
		sameTenant := s.OwnerID == userID
		if !sameTenant && tenantID != "" && m.UserTenantFn != nil {
			sameTenant = m.UserTenantFn(s.OwnerID) == tenantID
		}
		if !sameTenant {
			continue
		}
		if isTenantAdmin || s.OwnerID == userID {
			out = append(out, s)
			continue
		}
		if linked != nil {
			if _, ok := linked[s.ID]; ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func (m *SessionManager) snapshotEvents(userID string, isAdmin bool) []any {
	return []any{map[string]any{"type": "session-list", "sessions": m.infosFor(userID, isAdmin)}}
}

// ownerOf returns the owner of a session, or "" if unknown.
func (m *SessionManager) ownerOf(sessionID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.sessions[sessionID]; ok {
		return s.ownerID
	}
	return ""
}

// tenantOf returns the tenant root that owns a session, or "" if unknown.
func (m *SessionManager) tenantOf(sessionID string) string {
	owner := m.ownerOf(sessionID)
	if owner == "" {
		return ""
	}
	if m.UserTenantFn != nil {
		if t := m.UserTenantFn(owner); t != "" {
			return t
		}
	}
	return owner
}

// canAccess returns true when the user owns the session, is admin (super or
// tenant admin), belongs to the same tenant with an explicit link, or is a
// tenant admin whose company owns the session.
func (m *SessionManager) canAccess(sessionID, userID string, isAdmin bool) bool {
	if isAdmin {
		return true
	}
	owner := m.ownerOf(sessionID)
	if owner == "" {
		return false
	}
	if owner == userID {
		return true
	}
	// Same tenant?
	if m.UserTenantFn != nil {
		if t := m.UserTenantFn(userID); t != "" && t == m.UserTenantFn(owner) {
			// Tenant admin sees every company connection.
			if m.IsAdminRoleFn != nil && m.IsAdminRoleFn(userID) {
				return true
			}
			// Sub-user needs an explicit link.
			if m.UserSessionsFn != nil {
				for _, id := range m.UserSessionsFn(userID) {
					if id == sessionID {
						return true
					}
				}
			}
		}
	}
	return false
}

func (m *SessionManager) Restore(ctx context.Context) error {
	rows, err := m.store.list(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Mode == "cloud" {
			s := newCloudSession(m, row)
			m.register(s)
			continue
		}
		if row.JID == "" {
			_ = m.store.delete(ctx, row.ID)
			continue
		}
		jid, err := types.ParseJID(row.JID)
		if err != nil {
			m.log.Warn("dropping session with unparseable jid", "session", row.ID, "jid", row.JID)
			_ = m.store.delete(ctx, row.ID)
			continue
		}
		device, err := m.container.GetDevice(ctx, jid)
		if err != nil || device == nil {
			m.log.Warn("dropping session with no stored device", "session", row.ID, "jid", row.JID, "err", err)
			_ = m.store.delete(ctx, row.ID)
			continue
		}
		client := whatsmeow.NewClient(device, m.waLogger)
		s := newSession(m, row.ID, row.Name, client)
		s.ownerID = row.OwnerID
		s.color = row.Color
		s.isDefault = row.IsDefault
		s.allowGroups = row.AllowGroups
		s.integrationToken = row.IntegrationToken
		s.queueID = row.QueueID
		s.redirectMinutes = row.RedirectMinutes
		s.flowID = row.FlowID
		s.chatFlowID = row.ChatFlowID
		s.greetingMessage = row.GreetingMessage
		s.completionMessage = row.CompletionMessage
		s.outOfHoursMessage = row.OutOfHoursMessage
		s.surveyEnabled = row.SurveyEnabled
		s.surveyPrompt = row.SurveyPrompt
		// Migration: older builds stored either a URA or a chatbot in flow_id.
		// Split that into voice flow_id and chat_flow_id at runtime so calls only
		// run URA flows and WhatsApp text only runs chatbot flows.
		if strings.TrimSpace(s.flowID) != "" && strings.TrimSpace(s.chatFlowID) == "" && m.flowExec != nil && m.flowExec.store != nil {
			if f, err := m.flowExec.store.Get(ctx, s.flowID); err == nil && f != nil && flowKind(f) == "chat" {
				s.chatFlowID = s.flowID
				s.flowID = ""
				if err := m.store.setFlowIDs(ctx, row.ID, "", s.chatFlowID); err != nil {
					m.log.Warn("split legacy chat flow failed", "session", row.ID, "flow", s.chatFlowID, "err", err)
				} else {
					m.log.Info("split legacy chat flow", "session", row.ID, "chat_flow", s.chatFlowID, "name", f.Name)
				}
			}
		}
		// A conexão só pode ter URA quando o operador vinculou explicitamente no
		// cadastro. Versões antigas auto-vinculavam qualquer fluxo inbound do dono;
		// isso fazia chamada sem URA cair ativa. Não fazemos mais auto-bind.
		if strings.TrimSpace(s.chatFlowID) != "" && strings.TrimSpace(s.flowID) != "" {
			oldVoice := s.flowID
			s.flowID = ""
			if err := m.store.setFlowIDs(ctx, row.ID, "", s.chatFlowID); err != nil {
				m.log.Warn("clear hidden voice flow failed", "session", row.ID, "flow", oldVoice, "err", err)
			} else {
				m.log.Info("cleared hidden voice flow because chat flow is selected", "session", row.ID, "flow", oldVoice)
			}
		}
		m.register(s)
		if err := s.connect(ctx); err != nil {
			m.log.Error("session connect failed", "session", row.ID, "err", err)
		}
	}
	m.broker.emitSessionList(m.infos())
	m.log.Info("sessions restored", "count", len(m.infos()))
	return nil
}

func newCloudSession(m *SessionManager, row sessionRow) *Session {
	device := m.container.NewDevice()
	client := whatsmeow.NewClient(device, m.waLogger)
	s := newSession(m, row.ID, row.Name, client)
	s.ownerID = row.OwnerID
	s.color = row.Color
	s.isDefault = row.IsDefault
	s.allowGroups = row.AllowGroups
	s.integrationToken = row.IntegrationToken
	s.queueID = row.QueueID
	s.redirectMinutes = row.RedirectMinutes
	s.flowID = row.FlowID
	s.chatFlowID = row.ChatFlowID
	s.greetingMessage = row.GreetingMessage
	s.completionMessage = row.CompletionMessage
	s.outOfHoursMessage = row.OutOfHoursMessage
	s.surveyEnabled = row.SurveyEnabled
	s.surveyPrompt = row.SurveyPrompt
	s.mode = "cloud"
	s.cloudPhoneID = row.CloudPhoneID
	s.cloudWABAID = row.CloudWABAID
	s.cloudConfigured = row.CloudTokenEnc != ""
	s.auth = AuthSnapshot{State: "open", Paired: true}
	return s
}

func (m *SessionManager) ensureCloudSession(row sessionRow) *Session {
	s := newCloudSession(m, row)
	m.mu.Lock()
	old := m.sessions[row.ID]
	if old != nil && old.mode != "cloud" {
		old.shutdown()
	}
	m.sessions[row.ID] = s
	found := false
	for _, existing := range m.order {
		if existing == row.ID {
			found = true
			break
		}
	}
	if !found {
		m.order = append(m.order, row.ID)
	}
	m.mu.Unlock()
	return s
}

func (m *SessionManager) EnableCloud(ctx context.Context, id, phoneID, wabaID, token, appSecret, verifyToken string) error {
	if err := m.store.setCloudCreds(ctx, id, phoneID, wabaID, token, appSecret, verifyToken); err != nil {
		return err
	}
	row, err := m.lookupRow(id)
	if err != nil {
		return err
	}
	if row == nil {
		return fmt.Errorf("no session %s", id)
	}
	m.mu.Lock()
	old := m.sessions[id]
	if old != nil {
		old.shutdown()
	}
	m.sessions[id] = newCloudSession(m, *row)
	found := false
	for _, existing := range m.order {
		if existing == id {
			found = true
			break
		}
	}
	if !found {
		m.order = append(m.order, id)
	}
	m.mu.Unlock()
	m.broker.emitSessionList(m.infos())
	return nil
}

func (m *SessionManager) Create(name, ownerID string) (string, error) {
	id := newSessionID()
	if err := m.store.insertWithOwner(m.appCtx, id, name, ownerID); err != nil {
		return "", err
	}
	// Read back persisted row to capture generated integration token + defaults.
	row, _ := m.lookupRow(id)
	device := m.container.NewDevice()
	client := whatsmeow.NewClient(device, m.waLogger)
	s := newSession(m, id, name, client)
	s.ownerID = ownerID
	if row != nil {
		s.color = row.Color
		s.allowGroups = row.AllowGroups
		s.integrationToken = row.IntegrationToken
	} else {
		s.color = "#57adf8"
	}
	m.register(s)
	m.broker.emitSessionList(m.infos())
	if err := s.startPairing(m.appCtx); err != nil {
		m.log.Error("start pairing failed", "session", id, "err", err)
		return "", fmt.Errorf("start pairing: %w", err)
	}
	m.log.Info("session created", "session", id, "name", name)
	return id, nil
}

func (m *SessionManager) lookupRow(id string) (*sessionRow, error) {
	rows, err := m.store.list(m.appCtx)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if rows[i].ID == id {
			return &rows[i], nil
		}
	}
	return nil, nil
}

func (m *SessionManager) Delete(ctx context.Context, id string) error {
	s, ok := m.Get(id)
	if !ok {
		return fmt.Errorf("no session %s", id)
	}
	if s.client.Store.ID != nil {
		if err := s.client.Logout(ctx); err != nil {
			m.log.Warn("logout failed; deleting locally", "session", id, "err", err)
			_ = m.container.DeleteDevice(ctx, s.client.Store)
		}
	} else {
		s.client.Disconnect()
		_ = m.container.DeleteDevice(ctx, s.client.Store)
	}
	s.teardownAllCalls()
	m.unregister(id)
	_ = m.store.delete(ctx, id)
	m.broker.emitSessionList(m.infos())
	m.log.Info("session deleted", "session", id)
	return nil
}

func (m *SessionManager) Logout(ctx context.Context, id string) error {
	s, ok := m.Get(id)
	if !ok {
		return fmt.Errorf("no session %s", id)
	}
	if s.client.Store.ID != nil {
		if err := s.client.Logout(ctx); err != nil {
			m.log.Warn("logout failed", "session", id, "err", err)
		}
	}
	s.replaceClient(whatsmeow.NewClient(m.container.NewDevice(), m.waLogger))
	_ = m.store.setJID(ctx, id, "")
	s.setAuth(AuthSnapshot{State: "logged_out", Paired: false})
	m.log.Info("session disconnected", "session", id)
	return nil
}

func (m *SessionManager) Pair(id string) error {
	s, ok := m.Get(id)
	if !ok {
		return fmt.Errorf("no session %s", id)
	}
	if s.client.Store.ID != nil {
		return fmt.Errorf("session already paired")
	}
	s.replaceClient(whatsmeow.NewClient(m.container.NewDevice(), m.waLogger))
	if err := s.startPairing(m.appCtx); err != nil {
		return fmt.Errorf("start pairing: %w", err)
	}
	m.broker.emitSessionList(m.infos())
	m.log.Info("session re-pairing", "session", id)
	return nil
}

func (m *SessionManager) disconnectAll() {
	m.mu.RLock()
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.mu.RUnlock()
	for _, s := range all {
		s.shutdown()
	}
}

func (m *SessionManager) Update(ctx context.Context, id string, u sessionUpdate) error {
	s, ok := m.Get(id)
	if !ok {
		return fmt.Errorf("no session %s", id)
	}
	if err := m.store.update(ctx, id, u); err != nil {
		return err
	}
	s.mu.Lock()
	s.name = u.Name
	s.color = u.Color
	s.isDefault = u.IsDefault
	s.allowGroups = u.AllowGroups
	s.queueID = u.QueueID
	s.redirectMinutes = u.RedirectMinutes
	s.flowID = u.FlowID
	s.chatFlowID = u.ChatFlowID
	s.greetingMessage = u.GreetingMessage
	s.completionMessage = u.CompletionMessage
	s.outOfHoursMessage = u.OutOfHoursMessage
	s.surveyEnabled = u.SurveyEnabled
	s.surveyPrompt = u.SurveyPrompt
	s.mu.Unlock()
	m.broker.emitSessionList(m.infos())
	return nil
}
