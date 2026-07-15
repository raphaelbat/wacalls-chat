package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// ---- Live event capture ------------------------------------------------

// isGroupChatJID returns true when the JID belongs to a chat that should be
// rendered under the "Grupos" tab — classic groups, WhatsApp Channels
// (newsletters) and Community broadcast lists.
func isGroupChatJID(jid string) bool {
	return strings.HasSuffix(jid, "@g.us") ||
		strings.HasSuffix(jid, "@newsletter") ||
		strings.HasSuffix(jid, "@broadcast")
}

// handleWAMessage is called from Session.handleEvent for every *events.Message
// the whatsmeow client emits. It persists the message and broadcasts a SSE
// event so connected clients update in real time.
func (s *Session) handleWAMessage(evt *events.Message) {
	if s.mgr.messages == nil {
		return
	}
	row := messageRowFromEvent(s.id, evt)
	if row.ID == "" || row.ChatJID == "" {
		return
	}
	// Honor the per-connection "Receber mensagens de grupo" flag: when the
	// operator disables groups on the connection, inbound group/community
	// /channel messages are dropped entirely (not persisted, not broadcast)
	// so the Grupos tab stays empty until the flag is turned on.
	if isGroupChatJID(row.ChatJID) || evt.Info.IsGroup {
		s.mu.Lock()
		allow := s.allowGroups
		s.mu.Unlock()
		if !allow {
			return
		}
	}
	if err := s.mgr.messages.Insert(s.mgr.appCtx, row); err != nil {
		s.log.Warn("persist message failed", "err", err)
		return
	}
	s.upsertChatMeta(row, evt.Info.PushName, evt.Info.IsGroup)
	s.mgr.broker.emitMessage(row)
	// If the customer is answering a previously-sent satisfaction survey,
	// capture the score and skip the regular flow routing for this reply.
	if captureRatingReply(s.mgr.appCtx, s.mgr.chatMeta, s.log, s.id, row) {
		return
	}
	s.routeMessageToFlow(row)
	// Inbound rich media (image/video/audio/document/sticker) is downloaded
	// asynchronously so the chat list updates instantly, and a follow-up
	// SSE re-emit fills the preview URL once the bytes are on disk.
	// Always try to fetch media when we don't yet have a local copy. This
	// covers both inbound media and FromMe media sent from another linked
	// device (phone). Messages uploaded via our own /chats/.../media route
	// already stored a media_url, so the UPSERT-preserved row keeps it and
	// we'll skip the download via the empty-url check below.
	if isMediaKind(row.Kind) {
		go s.fetchIncomingMedia(row, evt)
	}
}

func isMediaKind(k string) bool {
	switch k {
	case "image", "video", "audio", "document", "sticker":
		return true
	}
	return false
}

// fetchIncomingMedia downloads media payloads via whatsmeow and persists the
// local URL on the message row so the UI can render an inline preview.
func (s *Session) fetchIncomingMedia(row MessageRow, evt *events.Message) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Warn("media download panic", "err", r)
		}
	}()
	// Skip when we already saved a local copy (e.g. media uploaded via our
	// own /chats/.../media endpoint).
	if s.mgr.messages != nil {
		if existing, ok, _ := s.mgr.messages.Get(s.mgr.appCtx, s.id, row.ID); ok && existing.MediaURL != "" {
			return
		}
	}
	ctx, cancel := context.WithTimeout(s.mgr.appCtx, 90*time.Second)
	defer cancel()
	data, err := s.client.DownloadAny(ctx, evt.Message)
	if err != nil || len(data) == 0 {
		s.log.Warn("media download failed", "id", row.ID, "err", err)
		return
	}
	name := mediaFileName(evt.Message, row)
	url := saveIncomingMedia(row.ID, name, data)
	if url == "" {
		return
	}
	if err := s.mgr.messages.UpdateMedia(ctx, s.id, row.ID, url, name, int64(len(data))); err != nil {
		s.log.Warn("media persist failed", "err", err)
		return
	}
	row.MediaURL = url
	row.FileName = name
	row.FileSize = int64(len(data))
	s.mgr.broker.emitMessage(row)
}

// mediaFileName picks a friendly filename for a media payload: document
// filename if present, otherwise <id>.<ext> guessed from the mime type.
func mediaFileName(m *waE2E.Message, row MessageRow) string {
	if d := m.GetDocumentMessage(); d != nil && d.GetFileName() != "" {
		return safeFileName(d.GetFileName())
	}
	ext := mediaExtForMime(row.MediaMime)
	if ext == "" {
		ext = extForKind(row.Kind)
	}
	base := row.ID
	if base == "" {
		base = "file"
	}
	return base + ext
}

func safeFileName(s string) string {
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	return s
}

func mediaExtForMime(mt string) string {
	mt = strings.TrimSpace(strings.SplitN(mt, ";", 2)[0])
	if mt == "" {
		return ""
	}
	if exts, err := mime.ExtensionsByType(mt); err == nil && len(exts) > 0 {
		return exts[0]
	}
	switch mt {
	case "audio/ogg", "audio/ogg; codecs=opus":
		return ".ogg"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "audio/mpeg":
		return ".mp3"
	}
	return ""
}

func extForKind(k string) string {
	switch k {
	case "image":
		return ".jpg"
	case "video":
		return ".mp4"
	case "audio":
		return ".ogg"
	case "sticker":
		return ".webp"
	}
	return ".bin"
}

func saveIncomingMedia(id, filename string, data []byte) string {
	dir := filepath.Join("media", "in")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	safe := safeFileName(filename)
	if safe == "" {
		safe = "file"
	}
	name := id + "-" + safe
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return ""
	}
	return "/api/media/in/" + name
}

// upsertChatMeta refreshes the chats table when a message arrives or is sent.
// It best-effort resolves group names through GetGroupInfo and bumps status
// to "waiting" for new inbound conversations.
func (s *Session) upsertChatMeta(row MessageRow, pushName string, isGroup bool) {
	if s.mgr == nil || s.mgr.chatMeta == nil {
		return
	}
	store := s.mgr.chatMeta
	ctx := s.mgr.appCtx
	existing, _, _ := store.Get(ctx, s.id, row.ChatJID)
	name := existing.Name
	isNewsletter := strings.HasSuffix(row.ChatJID, "@newsletter") || strings.HasSuffix(row.ChatJID, "@broadcast")
	if isGroup || strings.HasSuffix(row.ChatJID, "@g.us") || isNewsletter {
		isGroup = true
		if name == "" && !isNewsletter {
			if jid, err := types.ParseJID(row.ChatJID); err == nil {
				if gi, err := s.client.GetGroupInfo(ctx, jid); err == nil && gi != nil && gi.Name != "" {
					name = gi.Name
				}
			}
		}
		// Channels/Newsletters expose their title via push name on inbound.
		if name == "" && pushName != "" {
			name = pushName
		}
	} else if !row.FromMe && pushName != "" && pushName != name {
		name = pushName
	}
	status := existing.Status
	assigned := existing.AssignedUserID
	if isGroup {
		status = ChatStatusGroup
	} else if status == "" || status == ChatStatusClosed {
		// Brand-new or reopened conversations always land in the waiting
		// queue — even when the very first message was sent from the
		// operator's phone. An operator must explicitly "Atender" to move
		// the ticket to "atendendo".
		status = ChatStatusWaiting
		assigned = ""
	}
	m := ChatMeta{
		SessionID:      s.id,
		ChatJID:        row.ChatJID,
		Name:           name,
		IsGroup:        isGroup,
		Status:         status,
		AssignedUserID: assigned,
		UpdatedAt:      time.Now().UnixMilli(),
		AvatarURL:      existing.AvatarURL,
	}
	if err := store.Upsert(ctx, m); err != nil {
		s.log.Warn("chat meta upsert failed", "err", err)
		return
	}
	s.mgr.broker.emitChatMeta(m)
	// First time we see this chat (no prior status) → log a "created" lifecycle
	// event so the timeline shows "Conversa criada por · HH:MM" inline.
	if existing.Status == "" && s.mgr.chatMeta != nil {
		s.mgr.logChatEvent(ctx, m.SessionID, m.ChatJID, "created", "", "", "", m.UpdatedAt)
		if !isGroup && status == ChatStatusWaiting {
			s.mgr.logChatEvent(ctx, m.SessionID, m.ChatJID, "waiting", "", "", "", m.UpdatedAt)
		}
		// Pipeline: kick off "novo contato" automations the first time we
		// see this chat. Skip groups/channels — they're not sales contacts.
		if !isGroup && s.mgr.kanban != nil {
			s.mgr.kanban.TriggerAutomation(ctx, "new_contact", m.SessionID, m.ChatJID, m.Name)
		}
	}
	// Pipeline: "respondeu mensagem" fires whenever the contact (not us)
	// sends an inbound message in a 1:1 chat.
	if !row.FromMe && !isGroup && s.mgr.kanban != nil {
		s.mgr.kanban.TriggerAutomation(ctx, "replied", m.SessionID, m.ChatJID, m.Name)
	}
	// First-time enrichment: fetch avatar (and a better name) in the
	// background so the chat list shows a picture without user action.
	if existing.AvatarURL == "" {
		go s.fetchAvatarAsync(row.ChatJID)
	}
}

// fetchAvatarAsync resolves the avatar/name for a chat and persists it.
// Best-effort, swallows errors.
func (s *Session) fetchAvatarAsync(jid string) {
	if s.mgr == nil || s.mgr.chatMeta == nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.mgr.appCtx, 25*time.Second)
	defer cancel()
	cur, _, _ := s.mgr.chatMeta.Get(ctx, s.id, jid)
	name, avatar, err := resolveContactNameAndAvatar(ctx, s, jid, cur.Name, cur.AvatarURL)
	if err != nil || (name == cur.Name && avatar == cur.AvatarURL) {
		return
	}
	cur.Name = name
	cur.AvatarURL = avatar
	cur.UpdatedAt = time.Now().UnixMilli()
	if err := s.mgr.chatMeta.Upsert(ctx, cur); err == nil {
		s.mgr.broker.emitChatMeta(cur)
	}
}

// routeMessageToFlow forwards an inbound message to the FlowExecutor when the
// session has a flow configured (or a global "message" trigger flow exists).
// Skips outbound echoes, group chats when AllowGroups is off, and runs when
// the executor isn't ready.
func (s *Session) routeMessageToFlow(row MessageRow) {
	if row.FromMe {
		return
	}
	// Drop history-sync / offline replays: only trigger the bot for messages
	// that just arrived. Anything older than 2 minutes is treated as a
	// retroactive event (history sync, app-state replay, reconnect catch-up)
	// and must NOT cause the flow to fire — otherwise the bot will message
	// random contacts who never actually wrote to this number.
	if row.Ts > 0 && time.Now().UnixMilli()-row.Ts > 120_000 {
		return
	}
	// Ignore self-chat (operator sending to their own number) and any
	// broadcast / newsletter / status JIDs that should never trigger a bot.
	if s.client != nil && s.client.Store != nil && s.client.Store.ID != nil {
		if row.ChatJID == s.client.Store.ID.ToNonAD().String() || row.SenderJID == s.client.Store.ID.ToNonAD().String() {
			return
		}
	}
	if strings.Contains(row.ChatJID, "@broadcast") || strings.Contains(row.ChatJID, "@newsletter") || strings.HasSuffix(row.ChatJID, "@status") {
		return
	}
	s.mu.Lock()
	allowGroups := s.allowGroups
	ownerID := s.ownerID
	s.mu.Unlock()
	if isGroupChatJID(row.ChatJID) && !allowGroups {
		return
	}
	if s.mgr == nil || s.mgr.flowExec == nil {
		return
	}
	flowID := s.boundChatFlowID(s.mgr.appCtx)
	// Detecta keyword-match logo no início — palavras-chave são um gatilho
	// EXPLÍCITO do contato e devem ignorar a trava de "operador já respondeu".
	// O único bloqueio que se aplica a keyword é "atendimento aberto por
	// operador" (definido logo abaixo), conforme o texto da UI.
	var keywordFlowID string
	if row.Body != "" && !isGroupChatJID(row.ChatJID) && s.mgr.flowExec != nil {
		if kflow := s.mgr.flowExec.FindKeywordMatch(s.mgr.appCtx, ownerID, row.Body); kflow != nil {
			keywordFlowID = kflow.ID
		}
	}
	// Skip bot routing entirely once a human operator picked up the chat —
	// otherwise the connection's bound flow keeps replying on top of the
	// agent. Keyword/global flows são bloqueados pelo mesmo motivo.
	inAttendance := false
	if !isGroupChatJID(row.ChatJID) && s.mgr.chatMeta != nil {
		meta, ok, _ := s.mgr.chatMeta.Get(s.mgr.appCtx, s.id, row.ChatJID)
		inAttendance = ok && meta.Status == ChatStatusOpen && meta.AssignedUserID != ""
	}
	if inAttendance {
		hasPending := s.mgr.flowExec.HasPendingChat(s.id, row.ChatJID)
		// Atendimento aberto só bloqueia fluxos soltos/por palavra-chave.
		// Se o admin vinculou um fluxo de conversa na conexão, ele deve disparar
		// sempre que o contato falar nessa conexão; e se já existe uma etapa
		// aguardando resposta, ela precisa continuar mesmo com o ticket aberto.
		if flowID == "" && !hasPending {
			slog.Debug("flow routing skipped: chat in attendance", "session", s.id, "chat", row.ChatJID, "boundFlow", flowID, "keywordFlow", keywordFlowID)
			return
		}
	}
	// Keyword vence "prior outbound" — a palavra-chave é um pedido explícito
	// do contato para reiniciar o fluxo, mesmo que já tenha havido troca.
	if keywordFlowID != "" {
		slog.Info("flow routing: keyword match", "session", s.id, "chat", row.ChatJID, "flow", keywordFlowID)
		s.mgr.flowExec.StartForMessage(s.mgr.appCtx, s.id, ownerID, keywordFlowID, row)
		return
	}
	// "Operator already replied" gate: skip the bot only when there is
	// NO flow explicitly bound to this connection. When the user binds a
	// flow to the connection we trust the binding — otherwise the bot's
	// own outbound replies (from_me=1) would trip this check and freeze
	// multi-step flows after the first reply.
	if flowID == "" && !isGroupChatJID(row.ChatJID) && s.mgr.messages != nil {
		if had, _ := s.mgr.messages.HasPriorOutbound(s.mgr.appCtx, s.id, row.ChatJID, row.Ts); had {
			slog.Debug("flow routing skipped: prior outbound and no bound flow", "session", s.id, "chat", row.ChatJID)
			return
		}
	}
	if flowID == "" {
		// No flow bound and no keyword matched — nothing to run.
		return
	}
	slog.Info("flow routing: bound chat flow", "session", s.id, "chat", row.ChatJID, "flow", flowID)
	s.mgr.flowExec.StartForMessage(s.mgr.appCtx, s.id, ownerID, flowID, row)
}

// messageRowFromEvent maps a whatsmeow *events.Message into our local row.
func messageRowFromEvent(sessionID string, evt *events.Message) MessageRow {
	if evt == nil || evt.Message == nil {
		return MessageRow{}
	}
	kind, body, mime := classifyMessage(evt.Message)
	ts := evt.Info.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	return MessageRow{
		ID:         evt.Info.ID,
		SessionID:  sessionID,
		ChatJID:    evt.Info.Chat.String(),
		SenderJID:  evt.Info.Sender.String(),
		FromMe:     evt.Info.IsFromMe,
		Ts:         ts.UnixMilli(),
		Kind:       kind,
		Body:       body,
		MediaMime:  mime,
		SenderName: evt.Info.PushName,
	}
}

// classifyMessage returns (kind, displayBody, mediaMime).
//
// Cobre TODAS as variantes que chegam pela Cloud API oficial, anúncios
// Click-to-WhatsApp da Meta, templates HSM, botões/lista interativos e
// envelopes (Ephemeral / ViewOnce / DeviceSent / Edited). Sem isso, mensagens
// promocionais oficiais caíam em "unknown" e apareciam como "Mensagem".
func classifyMessage(m *waE2E.Message) (string, string, string) {
	if m == nil {
		return "unknown", "", ""
	}
	// --- Envelopes que embrulham outra mensagem: desembrulha e recursa ---
	if e := m.GetEphemeralMessage(); e != nil && e.GetMessage() != nil {
		return classifyMessage(e.GetMessage())
	}
	if v := m.GetViewOnceMessage(); v != nil && v.GetMessage() != nil {
		return classifyMessage(v.GetMessage())
	}
	if v := m.GetViewOnceMessageV2(); v != nil && v.GetMessage() != nil {
		return classifyMessage(v.GetMessage())
	}
	if v := m.GetViewOnceMessageV2Extension(); v != nil && v.GetMessage() != nil {
		return classifyMessage(v.GetMessage())
	}
	if d := m.GetDeviceSentMessage(); d != nil && d.GetMessage() != nil {
		return classifyMessage(d.GetMessage())
	}
	if p := m.GetEditedMessage(); p != nil && p.GetMessage() != nil {
		return classifyMessage(p.GetMessage())
	}
	if p := m.GetProtocolMessage(); p != nil && p.GetEditedMessage() != nil {
		return classifyMessage(p.GetEditedMessage())
	}
	switch {
	case strings.TrimSpace(m.GetConversation()) != "":
		return "text", m.GetConversation(), ""
	case m.GetExtendedTextMessage() != nil:
		// Extended text também carrega o texto de anúncios Click-to-WhatsApp
		// (CTA), respostas com quote e mensagens com link preview.
		ext := m.GetExtendedTextMessage()
		txt := strings.TrimSpace(ext.GetText())
		if txt == "" {
			txt = strings.TrimSpace(ext.GetMatchedText())
		}
		if txt == "" {
			txt = strings.TrimSpace(ext.GetDescription())
		}
		return "text", txt, ""
	case m.GetImageMessage() != nil:
		im := m.GetImageMessage()
		return "image", im.GetCaption(), im.GetMimetype()
	case m.GetVideoMessage() != nil:
		v := m.GetVideoMessage()
		return "video", v.GetCaption(), v.GetMimetype()
	case m.GetAudioMessage() != nil:
		return "audio", "", m.GetAudioMessage().GetMimetype()
	case m.GetDocumentMessage() != nil:
		d := m.GetDocumentMessage()
		body := d.GetCaption()
		if strings.TrimSpace(body) == "" {
			body = d.GetFileName()
		}
		return "document", body, d.GetMimetype()
	case m.GetStickerMessage() != nil:
		return "sticker", "", m.GetStickerMessage().GetMimetype()
	case m.GetLocationMessage() != nil:
		return "location", "", ""
	case m.GetLiveLocationMessage() != nil:
		return "location", strings.TrimSpace(m.GetLiveLocationMessage().GetCaption()), ""
	case m.GetContactMessage() != nil:
		c := m.GetContactMessage()
		if v := c.GetVcard(); v != "" {
			return "contact", v, ""
		}
		return "contact", c.GetDisplayName(), ""
	case m.GetContactsArrayMessage() != nil:
		ca := m.GetContactsArrayMessage()
		var parts []string
		for _, c := range ca.GetContacts() {
			if v := c.GetVcard(); v != "" {
				parts = append(parts, v)
			} else if dn := c.GetDisplayName(); dn != "" {
				parts = append(parts, "BEGIN:VCARD\nVERSION:3.0\nFN:"+dn+"\nEND:VCARD")
			}
		}
		if len(parts) > 0 {
			return "contact", strings.Join(parts, "\n---VCARD---\n"), ""
		}
		return "contact", ca.GetDisplayName(), ""
	case m.GetReactionMessage() != nil:
		r := m.GetReactionMessage()
		targetID := ""
		if k := r.GetKey(); k != nil {
			targetID = k.GetID()
		}
		return "reaction", r.GetText(), targetID

	// --- Mensagens oficiais / Cloud API / anúncios Meta ---

	case m.GetTemplateMessage() != nil:
		// Templates HSM aprovados pela Meta (campanhas, transacional, anúncios).
		tpl := m.GetTemplateMessage()
		if h := tpl.GetHydratedTemplate(); h != nil {
			if t := strings.TrimSpace(h.GetHydratedContentText()); t != "" {
				return "text", t, ""
			}
			if t := strings.TrimSpace(h.GetHydratedTitleText()); t != "" {
				return "text", t, ""
			}
			if t := strings.TrimSpace(h.GetHydratedFooterText()); t != "" {
				return "text", t, ""
			}
		}
		return "text", "", ""

	case m.GetInteractiveMessage() != nil:
		// Botões/CTA/Flow nativos da API oficial.
		i := m.GetInteractiveMessage()
		var parts []string
		if hdr := i.GetHeader(); hdr != nil {
			if t := strings.TrimSpace(hdr.GetTitle()); t != "" {
				parts = append(parts, t)
			}
			if t := strings.TrimSpace(hdr.GetSubtitle()); t != "" {
				parts = append(parts, t)
			}
		}
		if body := i.GetBody(); body != nil {
			if t := strings.TrimSpace(body.GetText()); t != "" {
				parts = append(parts, t)
			}
		}
		if ft := i.GetFooter(); ft != nil {
			if t := strings.TrimSpace(ft.GetText()); t != "" {
				parts = append(parts, t)
			}
		}
		return "text", strings.Join(parts, "\n\n"), ""

	case m.GetButtonsMessage() != nil:
		b := m.GetButtonsMessage()
		var parts []string
		if t := strings.TrimSpace(b.GetText()); t != "" {
			parts = append(parts, t)
		}
		if t := strings.TrimSpace(b.GetContentText()); t != "" {
			parts = append(parts, t)
		}
		if t := strings.TrimSpace(b.GetFooterText()); t != "" {
			parts = append(parts, t)
		}
		return "text", strings.Join(parts, "\n\n"), ""

	case m.GetListMessage() != nil:
		l := m.GetListMessage()
		var parts []string
		if t := strings.TrimSpace(l.GetTitle()); t != "" {
			parts = append(parts, t)
		}
		if t := strings.TrimSpace(l.GetDescription()); t != "" {
			parts = append(parts, t)
		}
		if t := strings.TrimSpace(l.GetFooterText()); t != "" {
			parts = append(parts, t)
		}
		return "text", strings.Join(parts, "\n\n"), ""

	case m.GetTemplateButtonReplyMessage() != nil:
		r := m.GetTemplateButtonReplyMessage()
		if t := strings.TrimSpace(r.GetSelectedDisplayText()); t != "" {
			return "text", t, ""
		}
		return "text", r.GetSelectedID(), ""

	case m.GetButtonsResponseMessage() != nil:
		r := m.GetButtonsResponseMessage()
		if t := strings.TrimSpace(r.GetSelectedDisplayText()); t != "" {
			return "text", t, ""
		}
		return "text", r.GetSelectedButtonID(), ""

	case m.GetListResponseMessage() != nil:
		r := m.GetListResponseMessage()
		if t := strings.TrimSpace(r.GetTitle()); t != "" {
			return "text", t, ""
		}
		if t := strings.TrimSpace(r.GetDescription()); t != "" {
			return "text", t, ""
		}
		if sr := r.GetSingleSelectReply(); sr != nil {
			return "text", sr.GetSelectedRowID(), ""
		}
		return "text", "", ""

	case m.GetInteractiveResponseMessage() != nil:
		r := m.GetInteractiveResponseMessage()
		if b := r.GetBody(); b != nil {
			if t := strings.TrimSpace(b.GetText()); t != "" {
				return "text", t, ""
			}
		}
		if nr := r.GetNativeFlowResponseMessage(); nr != nil {
			if t := strings.TrimSpace(nr.GetParamsJSON()); t != "" {
				return "text", t, ""
			}
		}
		return "text", "", ""
	}

	return "unknown", "", ""
}

// ---- HTTP routes -------------------------------------------------------

func (s *server) registerMessageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sessions/{sid}/chats", s.requireAuth(s.handleChatList))
	mux.HandleFunc("GET /api/sessions/{sid}/chats/{jid}/messages", s.requireAuth(s.handleChatMessages))
	mux.HandleFunc("POST /api/sessions/{sid}/chats/{jid}/send", s.requireAuth(s.handleChatSend))
	mux.HandleFunc("POST /api/sessions/{sid}/chats/{jid}/assign", s.requireAuth(s.handleChatAssign))
	mux.HandleFunc("POST /api/sessions/{sid}/chats/{jid}/close", s.requireAuth(s.handleChatClose))
	mux.HandleFunc("POST /api/sessions/{sid}/chats/{jid}/requeue", s.requireAuth(s.handleChatRequeue))
	mux.HandleFunc("POST /api/sessions/{sid}/chats/{jid}/transfer", s.requireAuth(s.handleChatTransfer))
	mux.HandleFunc("POST /api/sessions/{sid}/chats/{jid}/assign-to", s.requireAuth(s.handleChatAssignTo))
	mux.HandleFunc("POST /api/sessions/{sid}/chats/{jid}/media", s.requireAuth(s.handleChatMedia))
	mux.HandleFunc("POST /api/sessions/{sid}/chats/{jid}/contact", s.requireAuth(s.handleChatContact))
	mux.HandleFunc("POST /api/sessions/{sid}/chats/{jid}/read", s.requireAuth(s.handleChatRead))
	mux.HandleFunc("GET /api/sessions/{sid}/chats/{jid}/closures", s.requireAuth(s.handleChatClosures))
	mux.HandleFunc("GET /api/sessions/{sid}/chats/{jid}/events", s.requireAuth(s.handleChatEvents))
	mux.HandleFunc("POST /api/sessions/{sid}/chats/{jid}/sync", s.requireAuth(s.handleChatSync))
	mux.HandleFunc("POST /api/sessions/{sid}/chats/sync-all", s.requireAuth(s.handleChatSyncAll))
	mux.HandleFunc("GET /api/sessions/{sid}/lid-pn/{jid}", s.requireAuth(s.handleLidToPN))
	mux.HandleFunc("POST /api/sessions/{sid}/chats/{jid}/messages/{mid}/delete", s.requireAuth(s.handleMessageDelete))
	mux.HandleFunc("POST /api/sessions/{sid}/chats/{jid}/messages/{mid}/edit", s.requireAuth(s.handleMessageEdit))
	mux.HandleFunc("POST /api/sessions/{sid}/chats/{jid}/messages/{mid}/forward", s.requireAuth(s.handleMessageForward))
	mux.HandleFunc("POST /api/sessions/{sid}/chats/{jid}/note", s.requireAuth(s.handleChatNote))
	mux.HandleFunc("POST /api/sessions/{sid}/chats/{jid}/trigger-flow", s.requireAuth(s.handleChatTriggerFlow))
}

func (s *server) handleChatList(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	chats, err := s.messages.ListChats(r.Context(), sess.id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if s.chatMeta != nil {
		metas, _ := s.chatMeta.ListBySession(r.Context(), sess.id)
		unread, _ := s.messages.UnreadCounts(r.Context(), sess.id)
		for i := range chats {
			if m, ok := metas[chats[i].ChatJID]; ok {
				chats[i].Name = m.Name
				chats[i].IsGroup = m.IsGroup
				chats[i].Status = m.Status
				chats[i].AssignedUserID = m.AssignedUserID
				chats[i].LastReadTs = m.LastReadTs
				chats[i].AvatarURL = m.AvatarURL
			}
			chats[i].Unread = unread[chats[i].ChatJID]
			if chats[i].Status == "" {
				if isGroupChatJID(chats[i].ChatJID) {
					chats[i].Status = ChatStatusGroup
					chats[i].IsGroup = true
				} else if chats[i].LastFromMe {
					chats[i].Status = ChatStatusOpen
				} else {
					chats[i].Status = ChatStatusWaiting
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"chats": chats})
}

func (s *server) handleChatMessages(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	jid := r.PathValue("jid")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	rows, err := s.messages.ListMessages(r.Context(), sess.id, jid, limit, before)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": rows})
}

func (s *server) handleChatSend(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	u := currentUserFromReq(r)
	if u != nil {
		if qerr := s.enforceFreeTier(r.Context(), u.ID, u.IsAdmin(), "free_chats", s.weeklyUsage(r.Context(), u.ID, "free_chats")); qerr != nil {
			writeQuotaError(w, qerr)
			return
		}
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	body.Text = strings.TrimSpace(body.Text)
	if body.Text == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text required"})
		return
	}
	jid, err := parseChatJID(r.PathValue("jid"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if sess.mode == "cloud" {
		s.handleCloudChatSend(w, r, sess, jid, body.Text, u)
		return
	}
	if sess.client.Store.ID == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not paired"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	user := u
	if user == nil {
		user = currentUserFromReq(r)
	}
	finalText := applySignature(user, body.Text)
	msg := &waE2E.Message{Conversation: proto.String(finalText)}
	resp, err := sess.client.SendMessage(ctx, jid, msg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if user != nil {
		s.bumpWeeklyUsage(r.Context(), user.ID, "free_chats")
	}

	row := MessageRow{
		ID:        resp.ID,
		SessionID: sess.id,
		ChatJID:   jid.String(),
		SenderJID: jidOrEmpty(sess),
		FromMe:    true,
		Ts:        resp.Timestamp.UnixMilli(),
		Kind:      "text",
		Body:      finalText,
	}
	if row.Ts == 0 {
		row.Ts = time.Now().UnixMilli()
	}
	if err := s.messages.Insert(r.Context(), row); err != nil {
		s.log.Warn("persist outgoing message failed", "err", err)
	}
	s.markChatOpen(r.Context(), sess, row.ChatJID, user)
	s.broker.emitMessage(row)
	writeJSON(w, http.StatusOK, map[string]any{"message": row})
}

func (s *server) handleCloudChatSend(w http.ResponseWriter, r *http.Request, sess *Session, jid types.JID, text string, u *currentUser) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	client, _, err := s.cloudClientForSession(ctx, sess.id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	user := u
	if user == nil {
		user = currentUserFromReq(r)
	}
	finalText := applySignature(user, text)
	msgID, err := client.SendText(ctx, jid.User, finalText, false)
	if err != nil {
		s.log.Warn("cloud send text failed", "session", sess.id, "to", jid.User, "err", err)
		writeJSON(w, cloudSendStatus(err), map[string]string{"error": cloudSendFriendlyErr(err)})
		return
	}
	if msgID == "" {
		msgID = fmt.Sprintf("cloud-%d", time.Now().UnixNano())
	}
	if user != nil {
		s.bumpWeeklyUsage(r.Context(), user.ID, "free_chats")
	}
	now := time.Now().UnixMilli()
	row := MessageRow{
		ID:        msgID,
		SessionID: sess.id,
		ChatJID:   jid.String(),
		SenderJID: strings.TrimSpace(sess.cloudPhoneID),
		FromMe:    true,
		Ts:        now,
		Kind:      "text",
		Body:      finalText,
	}
	if row.SenderJID == "" {
		row.SenderJID = sess.id
	}
	if err := s.messages.Insert(r.Context(), row); err != nil {
		s.log.Warn("persist cloud outgoing message failed", "session", sess.id, "err", err)
	}
	s.markChatOpen(r.Context(), sess, row.ChatJID, user)
	s.broker.emitMessage(row)
	writeJSON(w, http.StatusOK, map[string]any{"message": row})
}

// applySignature is intentionally a no-op: the agent signature is appended
// client-side (at the bottom, in bold) so the operator can toggle it per
// conversation. Keeping the function lets the rest of the code stay unchanged
// while preventing a double signature when both sides tried to add it.
func applySignature(_ *currentUser, text string) string {
	return text
}

// markChatOpen flips a chat to "open" and assigns the sending agent. Groups
// keep their group status (they're not 1:1 tickets).
func (s *server) markChatOpen(ctx context.Context, sess *Session, jid string, u *currentUser) {
	if s.chatMeta == nil || u == nil {
		return
	}
	cur, _, _ := s.chatMeta.Get(ctx, sess.id, jid)
	if cur.IsGroup || isGroupChatJID(jid) {
		return
	}
	now := time.Now().UnixMilli()
	name := cur.Name
	m := ChatMeta{
		SessionID:      sess.id,
		ChatJID:        jid,
		Name:           name,
		IsGroup:        false,
		Status:         ChatStatusOpen,
		AssignedUserID: u.ID,
		UpdatedAt:      now,
	}
	if err := s.chatMeta.Upsert(ctx, m); err == nil {
		s.broker.emitChatMeta(m)
	}
	// Human took the chat — clear the bot's anti-loop marker so the next
	// inbound message after handoff doesn't restart the flow.
	if s.flowExec != nil {
		s.flowExec.ResetChatThrottle(sess.id, jid)
	}
}

func (s *server) handleChatAssign(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	u := currentUserFromReq(r)
	jid := r.PathValue("jid")
	s.markChatOpen(r.Context(), sess, jid, u)
	if u != nil {
		s.logChatEvent(r.Context(), sess.id, jid, "opened", u.ID, u.Email, "", time.Now().UnixMilli())
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (s *server) handleChatClose(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	jid := r.PathValue("jid")
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	body.Reason = strings.TrimSpace(body.Reason)
	now := time.Now().UnixMilli()
	u := currentUserFromReq(r)
	if err := s.chatMeta.SetStatus(r.Context(), sess.id, jid, ChatStatusClosed, "", now); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	entry := ChatCloseEntry{
		SessionID: sess.id,
		ChatJID:   jid,
		Reason:    body.Reason,
		ClosedAt:  now,
	}
	if u != nil {
		entry.UserID = u.ID
		entry.UserEmail = u.Email
	}
	if _, err := s.chatMeta.InsertClosure(r.Context(), entry); err != nil {
		s.log.Warn("persist chat closure failed", "err", err)
	}
	{
		uid, email := "", ""
		if u != nil {
			uid, email = u.ID, u.Email
		}
		s.logChatEvent(r.Context(), sess.id, jid, "closed", uid, email, body.Reason, now)
	}
	if m, ok, _ := s.chatMeta.Get(r.Context(), sess.id, jid); ok {
		s.broker.emitChatMeta(m)
	}
	// Closing the chat also resets the anti-loop marker so the next time
	// the contact writes the bot can start a fresh conversation.
	if s.flowExec != nil {
		s.flowExec.ResetChatThrottle(sess.id, jid)
	}
	// CSAT: if the connection has the satisfaction survey enabled, send the
	// configured text to the customer and arm a one-shot rating capture for
	// the next inbound message. Groups are skipped — surveys are 1:1 only.
	s.maybeSendSurvey(r.Context(), sess, jid)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

// defaultSurveyPrompt is used when the connection has surveys enabled but no
// custom text configured.
const defaultSurveyPrompt = "Avalie nosso atendimento respondendo apenas com um número:\n1 - Bom\n2 - Ruim\n3 - Péssimo"

// maybeSendSurvey delivers the CSAT prompt over WhatsApp and records the
// chat as "awaiting rating" so the next inbound text is parsed as a score.
func (s *server) maybeSendSurvey(ctx context.Context, sess *Session, rawJID string) {
	if sess == nil || s.chatMeta == nil {
		return
	}
	sess.mu.Lock()
	enabled := sess.surveyEnabled
	prompt := strings.TrimSpace(sess.surveyPrompt)
	sess.mu.Unlock()
	if !enabled {
		return
	}
	if isGroupChatJID(rawJID) {
		return
	}
	if sess.client == nil || sess.client.Store.ID == nil {
		return
	}
	if prompt == "" {
		prompt = defaultSurveyPrompt
	}
	jid, err := parseChatJID(rawJID)
	if err != nil {
		return
	}
	sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	msg := &waE2E.Message{Conversation: proto.String(prompt)}
	resp, err := sess.client.SendMessage(sendCtx, jid, msg)
	if err != nil {
		s.log.Warn("survey send failed", "err", err)
		return
	}
	row := MessageRow{
		ID:        resp.ID,
		SessionID: sess.id,
		ChatJID:   jid.String(),
		SenderJID: jidOrEmpty(sess),
		FromMe:    true,
		Ts:        resp.Timestamp.UnixMilli(),
		Kind:      "text",
		Body:      prompt,
	}
	if row.Ts == 0 {
		row.Ts = time.Now().UnixMilli()
	}
	if err := s.messages.Insert(ctx, row); err != nil {
		s.log.Warn("persist survey message failed", "err", err)
	}
	s.broker.emitMessage(row)
	if err := s.chatMeta.SetPendingRating(ctx, sess.id, jid.String(), row.Ts); err != nil {
		s.log.Warn("set pending rating failed", "err", err)
	}
}

// captureRatingReply inspects an inbound text message and, if a CSAT prompt
// is pending for the chat, parses the customer's answer into 1/2/3 and
// persists it. Returns true when the message was consumed as a rating.
func captureRatingReply(ctx context.Context, store *chatMetaStore, log *slog.Logger, sessionID string, row MessageRow) bool {
	if store == nil || row.FromMe || row.Kind != "text" {
		return false
	}
	if isGroupChatJID(row.ChatJID) {
		return false
	}
	if !store.HasPendingRating(ctx, sessionID, row.ChatJID) {
		return false
	}
	score := parseRatingScore(row.Body)
	if score == 0 {
		return false
	}
	_, err := store.InsertRating(ctx, ChatRating{
		SessionID: sessionID,
		ChatJID:   row.ChatJID,
		Score:     score,
		Reply:     strings.TrimSpace(row.Body),
		CreatedAt: time.Now().UnixMilli(),
	})
	if err != nil {
		if log != nil {
			log.Warn("persist rating failed", "err", err)
		}
		return false
	}
	store.ClearPendingRating(ctx, sessionID, row.ChatJID)
	return true
}

// parseRatingScore maps a customer's free-form reply ("1", "bom", "3 - péssimo")
// into a 1..3 score. Returns 0 when no signal is found.
func parseRatingScore(text string) int {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return 0
	}
	// First, look for a leading digit 1..3 (most common: customer just types "1").
	for _, r := range t {
		if r == '1' || r == '2' || r == '3' {
			return int(r - '0')
		}
		if r == ' ' || r == '-' || r == '.' || r == ')' || r == '(' {
			continue
		}
		break
	}
	switch {
	case strings.Contains(t, "bom") || strings.Contains(t, "boa") || strings.Contains(t, "otimo") || strings.Contains(t, "ótimo"):
		return 1
	case strings.Contains(t, "ruim"):
		return 2
	case strings.Contains(t, "pessimo") || strings.Contains(t, "péssimo") || strings.Contains(t, "horrivel") || strings.Contains(t, "horrível"):
		return 3
	}
	return 0
}

func (s *server) handleChatRead(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	jid := r.PathValue("jid")
	var body struct {
		Ts int64 `json:"ts"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	ts := body.Ts
	if ts <= 0 {
		latest, _ := s.messages.LatestTs(r.Context(), sess.id, jid)
		if latest > 0 {
			ts = latest
		} else {
			ts = time.Now().UnixMilli()
		}
	}
	m, err := s.chatMeta.MarkRead(r.Context(), sess.id, jid, ts)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.broker.emitChatMeta(m)
	writeJSON(w, http.StatusOK, map[string]any{"meta": m})
}

// handleChatRequeue puts a conversation back into the waiting queue and
// unassigns whoever currently owns it. Used by the "Devolver para fila" action.
func (s *server) handleChatRequeue(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	jid := r.PathValue("jid")
	if isGroupChatJID(jid) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "groups cannot be requeued"})
		return
	}
	now := time.Now().UnixMilli()
	if err := s.chatMeta.SetStatus(r.Context(), sess.id, jid, ChatStatusWaiting, "", now); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	{
		u := currentUserFromReq(r)
		uid, email := "", ""
		if u != nil {
			uid, email = u.ID, u.Email
		}
		s.logChatEvent(r.Context(), sess.id, jid, "requeued", uid, email, "", now)
	}
	if m, ok, _ := s.chatMeta.Get(r.Context(), sess.id, jid); ok {
		s.broker.emitChatMeta(m)
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

// handleChatTransfer reassigns the conversation to another operator.
// Body: {"userId":"..."}. Status becomes "open".
func (s *server) handleChatTransfer(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	jid := r.PathValue("jid")
	if isGroupChatJID(jid) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "groups cannot be transferred"})
		return
	}
	var body struct {
		UserID string `json:"userId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	body.UserID = strings.TrimSpace(body.UserID)
	if body.UserID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "userId required"})
		return
	}
	now := time.Now().UnixMilli()
	if err := s.chatMeta.SetStatus(r.Context(), sess.id, jid, ChatStatusOpen, body.UserID, now); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	{
		u := currentUserFromReq(r)
		uid, email := "", ""
		if u != nil {
			uid, email = u.ID, u.Email
		}
		s.logChatEvent(r.Context(), sess.id, jid, "transferred", uid, email, body.UserID, now)
	}
	if m, ok, _ := s.chatMeta.Get(r.Context(), sess.id, jid); ok {
		s.broker.emitChatMeta(m)
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (s *server) handleChatClosures(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	jid := r.PathValue("jid")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := s.chatMeta.ListClosures(r.Context(), sess.id, jid, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"closures": rows})
}

// handleChatAssignTo routes a freshly-opened conversation to a specific
// operator and/or queue chosen by the admin in the "Abrir atendimento" dialog.
// Body: {"userId":"...","queueId":"..."}. Either field may be empty (the
// chat will land in the waiting queue with no owner if both are empty).
func (s *server) handleChatAssignTo(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	jid := r.PathValue("jid")
	var body struct {
		UserID  string `json:"userId"`
		QueueID string `json:"queueId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	body.UserID = strings.TrimSpace(body.UserID)
	body.QueueID = strings.TrimSpace(body.QueueID)
	status := ChatStatusWaiting
	if body.UserID != "" {
		status = ChatStatusOpen
	}
	if isGroupChatJID(jid) {
		status = ChatStatusGroup
	}
	now := time.Now().UnixMilli()
	if err := s.chatMeta.SetAssignment(r.Context(), sess.id, jid, status, body.UserID, body.QueueID, now); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	u := currentUserFromReq(r)
	uid, email := "", ""
	if u != nil {
		uid, email = u.ID, u.Email
	}
	detail := body.UserID
	if body.QueueID != "" {
		if detail != "" {
			detail += " · "
		}
		detail += "queue=" + body.QueueID
	}
	if body.UserID != "" {
		s.logChatEvent(r.Context(), sess.id, jid, "transferred", uid, email, detail, now)
	} else {
		s.logChatEvent(r.Context(), sess.id, jid, "requeued", uid, email, detail, now)
	}
	if m, ok, _ := s.chatMeta.Get(r.Context(), sess.id, jid); ok {
		s.broker.emitChatMeta(m)
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (s *server) handleChatEvents(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	jid := r.PathValue("jid")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := s.chatMeta.ListEvents(r.Context(), sess.id, jid, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": rows})
}

// logChatEvent persists a single lifecycle row and broadcasts it via SSE so
// the active conversation timeline updates without a refetch.
func logChatEvent(ctx context.Context, store *chatMetaStore, broker *Broker, sessionID, jid, kind, userID, userEmail, detail string, ts int64) {
	if store == nil {
		return
	}
	if ts <= 0 {
		ts = time.Now().UnixMilli()
	}
	e := ChatEvent{
		SessionID: sessionID,
		ChatJID:   jid,
		Kind:      kind,
		UserID:    userID,
		UserEmail: userEmail,
		Detail:    detail,
		Ts:        ts,
	}
	id, err := store.InsertEvent(ctx, e)
	if err != nil {
		return
	}
	e.ID = id
	if broker != nil {
		broker.emitChatEvent(e)
	}
}

func (s *server) logChatEvent(ctx context.Context, sessionID, jid, kind, userID, userEmail, detail string, ts int64) {
	logChatEvent(ctx, s.chatMeta, s.broker, sessionID, jid, kind, userID, userEmail, detail, ts)
}

func (m *SessionManager) logChatEvent(ctx context.Context, sessionID, jid, kind, userID, userEmail, detail string, ts int64) {
	logChatEvent(ctx, m.chatMeta, m.broker, sessionID, jid, kind, userID, userEmail, detail, ts)
}

// handleChatMedia accepts a multipart upload (field "file") plus form fields
// `kind` (image|video|audio|document), `caption`, `filename`. It uploads to
// WhatsApp, saves a local copy under /api/media/<id-name> and persists the
// outgoing message.
func (s *server) handleChatMedia(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	// Cloud (Meta Official) sessions use a REST upload endpoint; the local
	// whatsmeow client is not paired so we route to the cloud send path.
	if sess.mode == "cloud" {
		s.handleCloudChatMedia(w, r, sess)
		return
	}
	if sess.client.Store.ID == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not paired"})
		return
	}
	u := currentUserFromReq(r)
	if u != nil {
		if qerr := s.enforceFreeTier(r.Context(), u.ID, u.IsAdmin(), "free_chats", s.weeklyUsage(r.Context(), u.ID, "free_chats")); qerr != nil {
			writeQuotaError(w, qerr)
			return
		}
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file required"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 64<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	kind := strings.ToLower(strings.TrimSpace(r.FormValue("kind")))
	if kind == "" {
		kind = "document"
	}
	caption := r.FormValue("caption")
	filename := r.FormValue("filename")
	if filename == "" {
		filename = header.Filename
	}
	mime := header.Header.Get("Content-Type")
	if mime == "" {
		mime = guessMimeForKind(kind)
	}
	// WhatsApp PTT/voice expects opus inside an Ogg container. Browsers
	// typically record `audio/webm;codecs=opus`. If ffmpeg is available,
	// transcode (or remux when already opus) to ogg/opus so the message
	// plays on every WhatsApp client.
	if kind == "audio" && !strings.Contains(strings.ToLower(mime), "ogg") {
		if converted, ok := transcodeToOggOpus(r.Context(), data); ok {
			data = converted
			mime = "audio/ogg; codecs=opus"
			if filename == "" || strings.HasSuffix(strings.ToLower(filename), ".webm") {
				filename = strings.TrimSuffix(filename, filepath.Ext(filename)) + ".ogg"
				if filename == ".ogg" || filename == "" {
					filename = fmt.Sprintf("audio-%d.ogg", time.Now().UnixMilli())
				}
			}
		}
	}
	jid, err := parseChatJID(r.PathValue("jid"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var appInfo whatsmeow.MediaType
	switch kind {
	case "audio":
		appInfo = whatsmeow.MediaAudio
	case "video":
		appInfo = whatsmeow.MediaVideo
	case "document":
		appInfo = whatsmeow.MediaDocument
	default:
		appInfo = whatsmeow.MediaImage
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	up, err := sess.client.Upload(ctx, data, appInfo)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("upload: %v", err)})
		return
	}
	msg := buildMediaMessage(kind, up, mime, caption, filename, uint64(len(data)))
	resp, err := sess.client.SendMessage(ctx, jid, msg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Save a local copy for instant playback / display.
	localURL := saveLocalMedia(resp.ID, filename, data)
	row := MessageRow{
		ID:        resp.ID,
		SessionID: sess.id,
		ChatJID:   jid.String(),
		SenderJID: jidOrEmpty(sess),
		FromMe:    true,
		Ts:        resp.Timestamp.UnixMilli(),
		Kind:      kind,
		Body:      caption,
		MediaMime: mime,
		MediaURL:  localURL,
		FileName:  filename,
		FileSize:  int64(len(data)),
	}
	if row.Ts == 0 {
		row.Ts = time.Now().UnixMilli()
	}
	_ = s.messages.Insert(r.Context(), row)
	s.markChatOpen(r.Context(), sess, row.ChatJID, currentUserFromReq(r))
	s.broker.emitMessage(row)
	if u != nil {
		s.bumpWeeklyUsage(r.Context(), u.ID, "free_chats")
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": row, "url": localURL})
}

func saveLocalMedia(id, filename string, data []byte) string {
	dir := filepath.Join("media", "out")
	_ = os.MkdirAll(dir, 0o755)
	safe := strings.ReplaceAll(filename, "/", "_")
	if safe == "" {
		safe = "file"
	}
	name := id + "-" + safe
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return ""
	}
	return "/api/media/out/" + name
}

// transcodeToOggOpus uses ffmpeg (if installed) to remux/transcode arbitrary
// browser-recorded audio into the ogg/opus container WhatsApp expects for
// PTT messages. Returns (nil, false) when ffmpeg is missing or the
// conversion fails — callers should fall back to the original bytes.
func transcodeToOggOpus(ctx context.Context, data []byte) ([]byte, bool) {
	bin, err := exec.LookPath("ffmpeg")
	if err != nil || bin == "" {
		return nil, false
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin,
		"-hide_banner", "-loglevel", "error",
		"-i", "pipe:0",
		"-vn",
		"-c:a", "libopus",
		"-b:a", "32k",
		"-ac", "1",
		"-ar", "48000",
		"-f", "ogg",
		"pipe:1",
	)
	cmd.Stdin = strings.NewReader(string(data))
	var out, errBuf strings.Builder
	cmd.Stdout = &writerAdapter{&out}
	cmd.Stderr = &writerAdapter{&errBuf}
	if err := cmd.Run(); err != nil {
		return nil, false
	}
	b := []byte(out.String())
	if len(b) < 64 {
		return nil, false
	}
	return b, true
}

// writerAdapter lets us capture binary ffmpeg output into a strings.Builder
// without an extra bytes.Buffer import elsewhere.
type writerAdapter struct{ b *strings.Builder }

func (w *writerAdapter) Write(p []byte) (int, error) { w.b.Write(p); return len(p), nil }

// handleChatContact sends a vCard contact card.
func (s *server) handleChatContact(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	if sess.client.Store.ID == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not paired"})
		return
	}
	var body struct {
		Name     string `json:"name"`
		Phone    string `json:"phone"`
		Contacts []struct {
			Name  string `json:"name"`
			Phone string `json:"phone"`
		} `json:"contacts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	// Normalize: collect a single list of contacts to send.
	type cc struct{ Name, Phone string }
	var list []cc
	if len(body.Contacts) > 0 {
		for _, c := range body.Contacts {
			n := strings.TrimSpace(c.Name)
			p := strings.TrimSpace(c.Phone)
			if n != "" && p != "" {
				list = append(list, cc{n, p})
			}
		}
	} else {
		n := strings.TrimSpace(body.Name)
		p := strings.TrimSpace(body.Phone)
		if n != "" && p != "" {
			list = append(list, cc{n, p})
		}
	}
	if len(list) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and phone required"})
		return
	}
	jid, err := parseChatJID(r.PathValue("jid"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	buildVCard := func(name, phone string) string {
		return "BEGIN:VCARD\nVERSION:3.0\nN:;" + name + ";;;\nFN:" + name +
			"\nTEL;type=CELL;type=VOICE;waid=" + sanitizePhone(phone) + ":" + phone + "\nEND:VCARD"
	}
	var msg *waE2E.Message
	var storedBody string
	if len(list) == 1 {
		vcard := buildVCard(list[0].Name, list[0].Phone)
		msg = &waE2E.Message{ContactMessage: &waE2E.ContactMessage{
			DisplayName: proto.String(list[0].Name),
			Vcard:       proto.String(vcard),
		}}
		storedBody = vcard
	} else {
		contacts := make([]*waE2E.ContactMessage, 0, len(list))
		vcards := make([]string, 0, len(list))
		names := make([]string, 0, len(list))
		for _, c := range list {
			v := buildVCard(c.Name, c.Phone)
			contacts = append(contacts, &waE2E.ContactMessage{
				DisplayName: proto.String(c.Name),
				Vcard:       proto.String(v),
			})
			vcards = append(vcards, v)
			names = append(names, c.Name)
		}
		msg = &waE2E.Message{ContactsArrayMessage: &waE2E.ContactsArrayMessage{
			DisplayName: proto.String(strings.Join(names, ", ")),
			Contacts:    contacts,
		}}
		storedBody = strings.Join(vcards, "\n---VCARD---\n")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	resp, err := sess.client.SendMessage(ctx, jid, msg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	row := MessageRow{
		ID: resp.ID, SessionID: sess.id, ChatJID: jid.String(), SenderJID: jidOrEmpty(sess),
		FromMe: true, Ts: resp.Timestamp.UnixMilli(), Kind: "contact", Body: storedBody,
	}
	if row.Ts == 0 {
		row.Ts = time.Now().UnixMilli()
	}
	_ = s.messages.Insert(r.Context(), row)
	s.markChatOpen(r.Context(), sess, row.ChatJID, currentUserFromReq(r))
	s.broker.emitMessage(row)
	writeJSON(w, http.StatusOK, map[string]any{"message": row})
}

func sanitizePhone(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func parseChatJID(s string) (types.JID, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return types.JID{}, errors.New("missing jid")
	}
	if strings.Contains(s, "@") {
		return types.ParseJID(s)
	}
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
	if digits == "" {
		return types.JID{}, errors.New("invalid jid")
	}
	return types.NewJID(digits, types.DefaultUserServer), nil
}

func jidOrEmpty(s *Session) string {
	if id := s.client.Store.ID; id != nil {
		return id.String()
	}
	return ""
}

// handleMessageDelete revokes (deletes for everyone) a previously sent
// message via whatsmeow's BuildRevoke and marks the local row as deleted.
// Only messages we sent ourselves can be revoked.
func (s *server) handleMessageDelete(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	if sess.client.Store.ID == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not paired"})
		return
	}
	mid := r.PathValue("mid")
	row, ok, err := s.messages.Get(r.Context(), sess.id, mid)
	if err != nil || !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "message not found"})
		return
	}
	if !row.FromMe {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "só é possível apagar mensagens enviadas por você"})
		return
	}
	chat, err := parseChatJID(row.ChatJID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	sender := *sess.client.Store.ID
	revoke := sess.client.BuildRevoke(chat, sender, mid)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if _, err := sess.client.SendMessage(ctx, chat, revoke); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.messages.MarkDeleted(r.Context(), sess.id, mid); err != nil {
		s.log.Warn("mark deleted failed", "err", err)
	}
	row.Body = ""
	row.Deleted = true
	s.broker.emitMessage(row)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

// handleMessageEdit rewrites the text of a previously sent message.
// WhatsApp only allows editing within ~15 minutes; the API returns whatever
// error whatsmeow surfaces if the window has passed.
func (s *server) handleMessageEdit(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	if sess.client.Store.ID == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not paired"})
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	body.Text = strings.TrimSpace(body.Text)
	if body.Text == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text required"})
		return
	}
	mid := r.PathValue("mid")
	row, ok, err := s.messages.Get(r.Context(), sess.id, mid)
	if err != nil || !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "message not found"})
		return
	}
	if !row.FromMe || row.Kind != "text" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "apenas mensagens de texto enviadas por você podem ser editadas"})
		return
	}
	chat, err := parseChatJID(row.ChatJID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	newMsg := &waE2E.Message{Conversation: proto.String(body.Text)}
	edit := sess.client.BuildEdit(chat, mid, newMsg)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if _, err := sess.client.SendMessage(ctx, chat, edit); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.messages.UpdateBody(r.Context(), sess.id, mid, body.Text); err != nil {
		s.log.Warn("update body failed", "err", err)
	}
	row.Body = body.Text
	row.Edited = true
	s.broker.emitMessage(row)
	writeJSON(w, http.StatusOK, map[string]any{"message": row})
}

// handleMessageForward re-sends an existing text message to one or more
// other chats. Media forwarding requires re-uploading, which we skip in
// this minimal flow.
func (s *server) handleMessageForward(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	if sess.client.Store.ID == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not paired"})
		return
	}
	var body struct {
		Targets []string `json:"targets"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if len(body.Targets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "informe ao menos um destino"})
		return
	}
	mid := r.PathValue("mid")
	src, ok, err := s.messages.Get(r.Context(), sess.id, mid)
	if err != nil || !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "message not found"})
		return
	}
	if src.Deleted {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mensagem indisponível"})
		return
	}
	if src.Kind != "text" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "apenas mensagens de texto podem ser encaminhadas por aqui"})
		return
	}
	text := src.Body
	if strings.TrimSpace(text) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mensagem vazia"})
		return
	}
	sent := 0
	var failures []string
	for _, t := range body.Targets {
		target, err := parseChatJID(t)
		if err != nil {
			failures = append(failures, t+": "+err.Error())
			continue
		}
		msg := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(text),
			ContextInfo: &waE2E.ContextInfo{
				IsForwarded:     proto.Bool(true),
				ForwardingScore: proto.Uint32(1),
			},
		}}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		resp, err := sess.client.SendMessage(ctx, target, msg)
		cancel()
		if err != nil {
			failures = append(failures, t+": "+err.Error())
			continue
		}
		row := MessageRow{
			ID: resp.ID, SessionID: sess.id, ChatJID: target.String(),
			SenderJID: jidOrEmpty(sess), FromMe: true,
			Ts: resp.Timestamp.UnixMilli(), Kind: "text", Body: text,
		}
		if row.Ts == 0 {
			row.Ts = time.Now().UnixMilli()
		}
		_ = s.messages.Insert(r.Context(), row)
		s.markChatOpen(r.Context(), sess, row.ChatJID, currentUserFromReq(r))
		s.broker.emitMessage(row)
		sent++
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": sent, "errors": failures})
}

// handleChatNote stores an internal "private note" attached to a chat. The
// note is never sent to the WhatsApp peer — it's saved with kind="note" and
// broadcast via SSE so every operator viewing the chat sees it instantly.
func (s *server) handleChatNote(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	body.Text = strings.TrimSpace(body.Text)
	if body.Text == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text required"})
		return
	}
	jidStr := r.PathValue("jid")
	user := currentUserFromReq(r)
	row := MessageRow{
		ID:         "note_" + newRunID(),
		SessionID:  sess.id,
		ChatJID:    jidStr,
		SenderJID:  user.ID,
		FromMe:     true,
		Ts:         time.Now().UnixMilli(),
		Kind:       "note",
		Body:       body.Text,
		SenderName: user.Email,
	}
	if err := s.messages.Insert(r.Context(), row); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.broker.emitMessage(row)
	writeJSON(w, http.StatusOK, map[string]any{"message": row})
}

// handleChatTriggerFlow kicks off a flow execution against the current chat,
// using the last inbound message as the synthetic trigger payload. This lets
// operators manually "dispatch" a flow from the chat surface.
func (s *server) handleChatTriggerFlow(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	var body struct {
		FlowID string `json:"flowId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.FlowID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "flowId required"})
		return
	}
	flow, err := s.flows.Get(r.Context(), body.FlowID)
	if err != nil || flow == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "flow not found"})
		return
	}
	u := currentUserFromReq(r)
	if !u.IsAdmin() && flow.OwnerID != u.ID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if s.flowExec == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "flow executor unavailable"})
		return
	}
	jidStr := r.PathValue("jid")
	// Build a synthetic MessageRow seeded with the last known inbound body so
	// the flow has something to work with. Falls back to an empty trigger.
	seed := MessageRow{
		ID:        "manual_" + newRunID(),
		SessionID: sess.id,
		ChatJID:   jidStr,
		SenderJID: jidStr,
		FromMe:    false,
		Ts:        time.Now().UnixMilli(),
		Kind:      "text",
		Body:      "",
	}
	if rows, err := s.messages.ListMessages(r.Context(), sess.id, jidStr, 1, 0); err == nil && len(rows) > 0 {
		last := rows[len(rows)-1]
		seed.Body = last.Body
		seed.Kind = last.Kind
		if last.SenderJID != "" {
			seed.SenderJID = last.SenderJID
		}
	}
	// Manual trigger: debounce 10s per chat so double-clicks (or repeated
	// requests from the UI) don't stack two parallel runs of the same
	// flow and produce duplicate outbound messages.
	if s.flowExec.ManualDebounce(sess.id, jidStr) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "fluxo já foi disparado há instantes — aguarde alguns segundos"})
		return
	}
	// Clear anti-loop throttle AND any pending chat_wait state so the
	// flow truly restarts from the beginning, and bypass the
	// in-attendance / prior-outbound guards.
	s.flowExec.ResetChatThrottle(sess.id, jidStr)
	go s.flowExec.StartForMessage(s.sessions.appCtx, sess.id, flow.OwnerID, flow.ID, seed)

	// Drop a visible note so the operator knows the flow was fired.
	note := MessageRow{
		ID:         "note_" + newRunID(),
		SessionID:  sess.id,
		ChatJID:    jidStr,
		SenderJID:  u.ID,
		FromMe:     true,
		Ts:         time.Now().UnixMilli(),
		Kind:       "note",
		Body:       "▶ Fluxo disparado: " + flow.Name,
		SenderName: u.Email,
	}
	_ = s.messages.Insert(r.Context(), note)
	s.broker.emitMessage(note)
	writeJSON(w, http.StatusOK, map[string]any{"ok": "1", "flowId": flow.ID})
}

// ---------- Cloud API (Official) send helpers ----------

// handleCloudChatMedia mirrors handleChatMedia for cloud-mode sessions. It
// uploads to Meta's /media endpoint, sends via /messages and persists a local
// copy so the message renders instantly in the chat UI.
func (s *server) handleCloudChatMedia(w http.ResponseWriter, r *http.Request, sess *Session) {
	u := currentUserFromReq(r)
	if u != nil {
		if qerr := s.enforceFreeTier(r.Context(), u.ID, u.IsAdmin(), "free_chats", s.weeklyUsage(r.Context(), u.ID, "free_chats")); qerr != nil {
			writeQuotaError(w, qerr)
			return
		}
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file required"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 64<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	kind := strings.ToLower(strings.TrimSpace(r.FormValue("kind")))
	if kind == "" {
		kind = "document"
	}
	caption := r.FormValue("caption")
	filename := r.FormValue("filename")
	if filename == "" {
		filename = header.Filename
	}
	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = guessMimeForKind(kind)
	}
	jid, err := parseChatJID(r.PathValue("jid"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	client, _, err := s.cloudClientForSession(ctx, sess.id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	mediaID, err := client.UploadMedia(ctx, filename, mimeType, data)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "upload: " + err.Error()})
		return
	}
	msgID, err := client.SendMedia(ctx, jid.User, cloudapiMediaPayloadFor(kind, mediaID, caption, filename))
	if err != nil {
		writeJSON(w, cloudSendStatus(err), map[string]string{"error": cloudSendFriendlyErr(err)})
		return
	}
	if msgID == "" {
		msgID = fmt.Sprintf("cloud-%d", time.Now().UnixNano())
	}
	// Save a local copy so the UI plays/shows the media immediately.
	localURL := saveLocalMedia(msgID, filename, data)
	row := MessageRow{
		ID:        msgID,
		SessionID: sess.id,
		ChatJID:   jid.String(),
		SenderJID: strings.TrimSpace(sess.cloudPhoneID),
		FromMe:    true,
		Ts:        time.Now().UnixMilli(),
		Kind:      kind,
		Body:      caption,
		MediaMime: mimeType,
		MediaURL:  localURL,
		FileName:  filename,
		FileSize:  int64(len(data)),
	}
	if row.SenderJID == "" {
		row.SenderJID = sess.id
	}
	if err := s.messages.Insert(r.Context(), row); err != nil {
		s.log.Warn("persist cloud outgoing media failed", "session", sess.id, "err", err)
	}
	s.markChatOpen(r.Context(), sess, row.ChatJID, u)
	s.broker.emitMessage(row)
	if u != nil {
		s.bumpWeeklyUsage(r.Context(), u.ID, "free_chats")
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": row, "url": localURL})
}
