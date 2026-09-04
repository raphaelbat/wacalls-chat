package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"text/template"
	"time"
)

// ---- Graph model --------------------------------------------------------

type FlowGraph struct {
	Nodes       []FlowNode       `json:"nodes"`
	Edges       []FlowEdge       `json:"edges"`
	StartNodeID string           `json:"startNodeId"`
	Voice       *FlowVoiceConfig `json:"voice,omitempty"`
	Kind        string           `json:"kind,omitempty"` // "voice" | "chat" (defaults to voice)
}

type FlowVoiceConfig struct {
	Provider         string `json:"provider"`
	VoiceID          string `json:"voiceId"`
	Model            string `json:"model,omitempty"`
	ElevenLabsAPIKey string `json:"elevenlabsApiKey,omitempty"`
}

type FlowNode struct {
	ID   string                 `json:"id"`
	Type string                 `json:"type"` // voice_menu | message | whatsapp_send | ai_agent | webhook | end | condition | delay | transfer
	Data map[string]interface{} `json:"data"`
}

type FlowEdge struct {
	ID           string `json:"id"`
	Source       string `json:"source"`
	Target       string `json:"target"`
	SourceHandle string `json:"sourceHandle,omitempty"` // for branches: "true"/"false", option keys, etc.
}

// ---- Executor -----------------------------------------------------------

type FlowExecutor struct {
	store *flowStore
	log   *slog.Logger

	bridge *FlowBridge
	broker *Broker
	tracer *flowTracer

	mu      sync.Mutex
	running map[string]context.CancelFunc // callID -> cancel

	// chatThrottle records when a chat flow last completed for a given
	// (sessionID|chatJID). Inbound messages within chatThrottleTTL are
	// ignored so the bot does not restart from the top on every reply
	// after it already finished its script.
	throttleMu  sync.Mutex
	chatLastEnd map[string]time.Time

	// chatWait keeps the live state for chat nodes that must wait for the
	// customer's next inbound message (chat_input, chat_menu, chat_msg_api).
	// Without this, the executor sent every prompt at once and then completed,
	// so the bound conversation flow appeared to "not fire" or restarted from
	// the first node on every customer reply.
	chatWaitMu sync.Mutex
	chatWait   map[string]*chatWaitState

	// Dedup/coalesce state: prevents the same inbound being processed
	// twice (whatsmeow can redeliver during history sync / reconnect)
	// and prevents repeated manual triggers from running the flow
	// concurrently and producing duplicate outbound messages.
	dedupMu      sync.Mutex
	recentMsgIDs map[string]time.Time // sessionID|chatJID|msgID -> seen
	chatRunning  map[string]time.Time // sessionID|chatJID -> run started
	chatManualAt map[string]time.Time // sessionID|chatJID -> last manual fire
}

func newFlowExecutor(store *flowStore, log *slog.Logger) *FlowExecutor {
	return &FlowExecutor{
		store:        store,
		log:          log,
		running:      map[string]context.CancelFunc{},
		chatLastEnd:  map[string]time.Time{},
		chatWait:     map[string]*chatWaitState{},
		recentMsgIDs: map[string]time.Time{},
		chatRunning:  map[string]time.Time{},
		chatManualAt: map[string]time.Time{},
	}
}

// markMsgSeen returns true when the (session,chat,msgID) tuple was already
// observed within the last 5 minutes. Callers should drop duplicates.
func (e *FlowExecutor) markMsgSeen(sessionID, chatJID, msgID string) bool {
	if e == nil || msgID == "" {
		return false
	}
	key := sessionID + "|" + chatJID + "|" + msgID
	now := time.Now()
	e.dedupMu.Lock()
	defer e.dedupMu.Unlock()
	if t, ok := e.recentMsgIDs[key]; ok && now.Sub(t) < 5*time.Minute {
		return true
	}
	// opportunistic GC
	if len(e.recentMsgIDs) > 2048 {
		for k, t := range e.recentMsgIDs {
			if now.Sub(t) > 10*time.Minute {
				delete(e.recentMsgIDs, k)
			}
		}
	}
	e.recentMsgIDs[key] = now
	return false
}

// acquireChatRun returns false when another run is already in-flight for the
// same (session,chat) within the last 30s. Avoids two parallel flow runs
// stomping on the same chatWait state (which produced duplicate prompts).
func (e *FlowExecutor) acquireChatRun(sessionID, chatJID string) bool {
	if e == nil || chatJID == "" {
		return true
	}
	key := sessionID + "|" + chatJID
	now := time.Now()
	e.dedupMu.Lock()
	defer e.dedupMu.Unlock()
	if t, ok := e.chatRunning[key]; ok && now.Sub(t) < 30*time.Second {
		return false
	}
	e.chatRunning[key] = now
	return true
}

func (e *FlowExecutor) releaseChatRun(sessionID, chatJID string) {
	if e == nil || chatJID == "" {
		return
	}
	key := sessionID + "|" + chatJID
	e.dedupMu.Lock()
	delete(e.chatRunning, key)
	e.dedupMu.Unlock()
}

// manualDebounce returns true when a manual trigger for the same chat fired
// in the last 10s. Prevents accidental double clicks from re-running the
// flow on top of itself.
func (e *FlowExecutor) ManualDebounce(sessionID, chatJID string) bool {
	if e == nil || chatJID == "" {
		return false
	}
	key := sessionID + "|" + chatJID
	now := time.Now()
	e.dedupMu.Lock()
	defer e.dedupMu.Unlock()
	if t, ok := e.chatManualAt[key]; ok && now.Sub(t) < 10*time.Second {
		return true
	}
	e.chatManualAt[key] = now
	return false
}

// chatThrottleTTL: how long after a chat flow completion we ignore further
// inbound messages from the same contact. The bot is "done" — a human
// operator picking up the chat (chatMeta.AssignedUserID) bypasses the
// throttle, and so does an explicit reset keyword ("reiniciar", "menu").
const chatThrottleTTL = 60 * time.Second

func chatThrottleKey(sessionID, chatJID string) string {
	return sessionID + "|" + chatJID
}

const chatWaitBranch = "__chat_wait__"

type chatWaitState struct {
	FlowID   string
	OwnerID  string
	NodeID   string
	NextID   string
	WaitKind string
	SaveAs   string
	Vars     map[string]interface{}
	Expires  time.Time
}

func chatWaitKey(sessionID, chatJID string) string {
	return sessionID + "|" + chatJID
}

func copyVars(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return map[string]interface{}{}
	}
	b, err := json.Marshal(in)
	if err != nil {
		out := make(map[string]interface{}, len(in))
		for k, v := range in {
			out[k] = v
		}
		return out
	}
	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil || out == nil {
		out = map[string]interface{}{}
	}
	return out
}

func (e *FlowExecutor) setChatWait(sessionID, chatJID string, st *chatWaitState) {
	if e == nil || st == nil || chatJID == "" {
		return
	}
	if st.Expires.IsZero() {
		st.Expires = time.Now().Add(24 * time.Hour)
	}
	e.chatWaitMu.Lock()
	e.chatWait[chatWaitKey(sessionID, chatJID)] = st
	e.chatWaitMu.Unlock()
}

func (e *FlowExecutor) popChatWait(sessionID, chatJID string) (*chatWaitState, bool) {
	if e == nil || chatJID == "" {
		return nil, false
	}
	key := chatWaitKey(sessionID, chatJID)
	e.chatWaitMu.Lock()
	st, ok := e.chatWait[key]
	if ok {
		delete(e.chatWait, key)
	}
	e.chatWaitMu.Unlock()
	if !ok || st == nil {
		return nil, false
	}
	if !st.Expires.IsZero() && time.Now().After(st.Expires) {
		return nil, false
	}
	return st, true
}

func (e *FlowExecutor) HasPendingChat(sessionID, chatJID string) bool {
	if e == nil || chatJID == "" {
		return false
	}
	key := chatWaitKey(sessionID, chatJID)
	e.chatWaitMu.Lock()
	defer e.chatWaitMu.Unlock()
	st, ok := e.chatWait[key]
	if !ok || st == nil {
		return false
	}
	if !st.Expires.IsZero() && time.Now().After(st.Expires) {
		delete(e.chatWait, key)
		return false
	}
	return true
}

// chatRecentlyCompleted reports true when StartForMessage should skip the
// inbound message because the flow already finished for this contact.
func (e *FlowExecutor) chatRecentlyCompleted(sessionID, chatJID string) bool {
	if e == nil || chatJID == "" {
		return false
	}
	e.throttleMu.Lock()
	defer e.throttleMu.Unlock()
	t, ok := e.chatLastEnd[chatThrottleKey(sessionID, chatJID)]
	if !ok {
		return false
	}
	if time.Since(t) > chatThrottleTTL {
		delete(e.chatLastEnd, chatThrottleKey(sessionID, chatJID))
		return false
	}
	return true
}

// markChatCompleted records that the flow finished for (sessionID,chatJID)
// so we won't retrigger it from scratch on the very next reply.
func (e *FlowExecutor) markChatCompleted(sessionID, chatJID string) {
	if e == nil || chatJID == "" {
		return
	}
	e.throttleMu.Lock()
	e.chatLastEnd[chatThrottleKey(sessionID, chatJID)] = time.Now()
	e.throttleMu.Unlock()
}

// ResetChatThrottle clears the completion marker so the next message starts
// the bot from scratch. Called when a human closes the chat or when the
// user requests a manual reset.
func (e *FlowExecutor) ResetChatThrottle(sessionID, chatJID string) {
	if e == nil || chatJID == "" {
		return
	}
	e.throttleMu.Lock()
	delete(e.chatLastEnd, chatThrottleKey(sessionID, chatJID))
	e.throttleMu.Unlock()
	e.chatWaitMu.Lock()
	delete(e.chatWait, chatWaitKey(sessionID, chatJID))
	e.chatWaitMu.Unlock()
}

// AttachBridge wires the executor to a FlowBridge that can talk to live
// calls and the WhatsApp client. Without it the voice / whatsapp nodes
// fall back to "stub" mode and only emit trace events.
func (e *FlowExecutor) AttachBridge(b *FlowBridge) {
	if e == nil {
		return
	}
	e.bridge = b
}

// AttachBroker wires the broker so the executor can emit user-visible
// `flow-skip` diagnostic events when an inbound call fails to trigger the
// URA (no bound flow, flow disabled, etc). Without it the executor logs
// only — the user has no way to know why the URA stayed silent.
func (e *FlowExecutor) AttachBroker(b *Broker) {
	if e == nil {
		return
	}
	e.broker = b
}

// AttachTracer wires the per-call correlation log used by /api/flows/trace.
// Without it Record* calls become no-ops.
func (e *FlowExecutor) AttachTracer(t *flowTracer) {
	if e == nil {
		return
	}
	e.tracer = t
}

func (e *FlowExecutor) trace(step FlowTraceStep) FlowTraceStep {
	if e == nil || e.tracer == nil {
		return step
	}
	return e.tracer.Record(step)
}

// emitFlowSkip publishes a diagnostic event scoped to the session owner so the
// frontend can show a toast like "URA não disparou: <motivo>". reason values
// are stable strings consumed by the UI (see client/src/lib/event-stream.ts).
// The traceId is included so the user can pivot to GET /api/flows/trace.
func (e *FlowExecutor) emitFlowSkip(sessionID, callID, flowID, reason, detail string) {
	traceID := ""
	if e != nil && e.tracer != nil {
		traceID = e.tracer.TraceIDFor(callID)
	}
	e.trace(FlowTraceStep{
		CallID: callID, SessionID: sessionID, TraceID: traceID,
		Level: "warn", Code: "skip", Message: reason,
		Data: map[string]any{"flowId": flowID, "detail": detail},
	})
	if e == nil || e.broker == nil {
		return
	}
	e.broker.deliverScoped(sessionID, map[string]any{
		"type":      "flow-skip",
		"sessionId": sessionID,
		"callId":    callID,
		"flowId":    flowID,
		"reason":    reason,
		"detail":    detail,
		"traceId":   traceID,
		"ts":        time.Now().UnixMilli(),
	})
}

// flowKind returns the flow's declared kind ("voice" or "chat"), defaulting to
// "voice" when the graph cannot be parsed or has no kind set. Used to keep
// chat-only flows from triggering on inbound calls and voice flows from
// triggering on inbound chat messages.
func flowKind(flow *FlowRow) string {
	if flow == nil {
		return "voice"
	}
	var g FlowGraph
	if err := json.Unmarshal([]byte(flow.Graph), &g); err != nil {
		return "voice"
	}
	k := strings.TrimSpace(strings.ToLower(g.Kind))
	if k == "chat" {
		return "chat"
	}
	// When the kind isn't explicitly set we default to "voice". A previous
	// heuristic inspected node types ("chat_*"/"ig_*") to guess chat, but
	// that misclassified hybrid/legacy URA flows and caused valid voice
	// flows bound to a connection/campaign to be skipped with
	// "flow_kind_mismatch". The Flow editor always writes an explicit
	// kind today, so the safe default is voice.
	return "voice"
}

// StartForCall is called when an inbound call is accepted. It loads the active
// inbound flow (if any) and executes it in a goroutine. Safe to call even when
// no flow is configured: it becomes a no-op.
func (e *FlowExecutor) StartForCall(parent context.Context, sessionID, callID, fromNumber, ownerID string, flowIDs ...string) {
	if e == nil {
		return
	}
	traceID := ""
	if e.tracer != nil {
		traceID = e.tracer.TraceIDFor(callID)
	}
	e.trace(FlowTraceStep{
		CallID: callID, SessionID: sessionID, OwnerID: ownerID, TraceID: traceID,
		Level: "info", Code: "start",
		Data: map[string]any{"flowIdHint": strings.Join(flowIDs, ","), "from": fromNumber},
	})
	// Idempotent: avoid double-start when both the local accept path
	// (inbound URA) and the remote events.CallAccept (outbound) fire for
	// the same callID.
	e.mu.Lock()
	if _, running := e.running[callID]; running {
		e.mu.Unlock()
		if e.log != nil {
			e.log.Info("flow skipped: already running", "callID", callID, "traceId", traceID)
		}
		e.trace(FlowTraceStep{
			CallID: callID, SessionID: sessionID, TraceID: traceID,
			Level: "info", Code: "skip", Message: "already_running",
		})
		return
	}
	e.mu.Unlock()
	var flow *FlowRow
	var err error
	flowID := ""
	if len(flowIDs) > 0 {
		flowID = strings.TrimSpace(flowIDs[0])
	}
	if flowID != "" {
		flow, err = e.store.Get(parent, flowID)
		switch {
		case err != nil:
			if e.log != nil {
				e.log.Warn("flow lookup failed", "callID", callID, "traceId", traceID, "flowID", flowID, "err", err)
			}
			e.emitFlowSkip(sessionID, callID, flowID, "flow_lookup_failed", err.Error())
			flow = nil
		case flow == nil:
			if e.log != nil {
				e.log.Warn("flow not found", "callID", callID, "traceId", traceID, "flowID", flowID)
			}
			e.emitFlowSkip(sessionID, callID, flowID, "flow_not_found", "fluxo vinculado não existe mais")
		case !flow.Enabled:
			if e.log != nil {
				e.log.Warn("flow disabled — skipping", "callID", callID, "traceId", traceID, "flowID", flowID, "name", flow.Name)
			}
			e.emitFlowSkip(sessionID, callID, flowID, "flow_disabled", "habilite o fluxo \""+flow.Name+"\" no FlowBuilder")
			flow = nil
		}
		// Chat-type flows must not run on inbound calls: they are
		// triggered by WhatsApp messages, not by voice. Skip silently
		// and let the call proceed without URA.
		if flow != nil && flowKind(flow) == "chat" {
			if e.log != nil {
				e.log.Info("flow skipped: chat flow on inbound call", "callID", callID, "traceId", traceID, "flowID", flow.ID, "name", flow.Name)
			}
			e.emitFlowSkip(sessionID, callID, flow.ID, "flow_kind_mismatch", "fluxo vinculado é de conversa (chatbot) e só dispara por mensagens")
			flow = nil
		}
	}
	// Strict opt-in: only run a voice flow when it's explicitly bound to
	// the connection (sessions.flow_id). We intentionally do NOT fall back
	// to "any enabled inbound voice flow" for the owner — that caused
	// unrelated connections to trigger flows by themselves.
	if flow != nil && flowKind(flow) == "chat" {
		// Defensive: never start a chat-kind flow from a call.
		flow = nil
	}
	if err != nil || flow == nil {
		if e.log != nil {
			e.log.Warn("flow skipped: no enabled inbound flow", "callID", callID, "traceId", traceID, "sessionFlowID", flowID, "ownerID", ownerID, "err", err)
		}
		// Only emit the generic "no inbound flow" reason when we didn't
		// already emit a more specific one above (flow_disabled etc).
		if flowID == "" {
			e.emitFlowSkip(sessionID, callID, flowID, "no_inbound_flow", "nenhum fluxo habilitado com gatilho \"inbound\"")
		}
		return
	}
	if e.log != nil {
		e.log.Info("flow start", "callID", callID, "traceId", traceID, "flowID", flow.ID, "trigger", flow.Trigger)
	}
	e.trace(FlowTraceStep{
		CallID: callID, SessionID: sessionID, OwnerID: ownerID, TraceID: traceID,
		Level: "info", Code: "resolved",
		Data: map[string]any{"flowId": flow.ID, "name": flow.Name, "trigger": flow.Trigger},
	})
	vars := map[string]interface{}{
		"call": map[string]interface{}{"from": fromNumber, "session": sessionID, "id": callID},
		"vars": map[string]interface{}{},
	}
	go e.runFlow(parent, flow, callID, sessionID, vars, false)
}

func chatMessageVars(sessionID string, row MessageRow) map[string]interface{} {
	return map[string]interface{}{
		"id":      row.ID,
		"chat":    row.ChatJID,
		"from":    row.SenderJID,
		"body":    row.Body,
		"kind":    row.Kind,
		"fromMe":  row.FromMe,
		"isGroup": strings.HasSuffix(row.ChatJID, "@g.us"),
		"ts":      row.Ts,
	}
}

func ensureFlowVars(vars map[string]interface{}) map[string]interface{} {
	if vars == nil {
		vars = map[string]interface{}{}
	}
	if _, ok := vars["vars"].(map[string]interface{}); !ok {
		vars["vars"] = map[string]interface{}{}
	}
	return vars
}

func flowNextOf(graph FlowGraph, nodeID, handle string) string {
	for _, ed := range graph.Edges {
		if ed.Source != nodeID {
			continue
		}
		if handle != "" && ed.SourceHandle != handle {
			continue
		}
		if handle == "" && ed.SourceHandle != "" && ed.SourceHandle != "out" {
			continue
		}
		return ed.Target
	}
	if handle != "" {
		for _, ed := range graph.Edges {
			if ed.Source == nodeID {
				return ed.Target
			}
		}
	}
	return ""
}

func extractInteractiveReplyValue(body string) string {
	body = strings.TrimSpace(body)
	if body == "" || !strings.HasPrefix(body, "{") {
		return body
	}
	var m map[string]interface{}
	if json.Unmarshal([]byte(body), &m) != nil {
		return body
	}
	for _, k := range []string{"id", "selected_id", "selectedId", "selectedRowId", "selected_row_id", "button_id", "buttonId", "name", "display_text", "displayText", "title", "text"} {
		if v, ok := m[k]; ok {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" {
				return s
			}
		}
	}
	return body
}

func matchChatOptionBranch(n FlowNode, body string) string {
	textRaw := extractInteractiveReplyValue(body)
	text := normalizeSpeech(textRaw)
	if text == "" {
		return ""
	}
	matchOne := func(idx int, key, label string, synonyms []string) (string, bool) {
		key = strings.TrimSpace(key)
		label = strings.TrimSpace(label)
		cands := []string{key, label, fmt.Sprintf("%d", idx+1)}
		cands = append(cands, ordinalSynonyms(idx+1)...)
		cands = append(cands, synonyms...)
		for _, c := range cands {
			cc := normalizeSpeech(c)
			if cc == "" {
				continue
			}
			if text == cc || wordIndex(text, cc) >= 0 {
				if key != "" {
					return key, true
				}
				return fmt.Sprintf("%d", idx+1), true
			}
		}
		return "", false
	}
	idx := 0
	if opts, ok := n.Data["options"].([]interface{}); ok {
		for _, raw := range opts {
			o, _ := raw.(map[string]interface{})
			if o == nil {
				continue
			}
			key, _ := o["key"].(string)
			label, _ := o["label"].(string)
			if label == "" {
				label, _ = o["text"].(string)
			}
			var syns []string
			for _, field := range []string{"synonyms", "phrases"} {
				if arr, ok := o[field].([]interface{}); ok {
					for _, v := range arr {
						if sv, ok := v.(string); ok {
							syns = append(syns, sv)
						}
					}
				}
			}
			if branch, ok := matchOne(idx, key, label, syns); ok {
				return branch
			}
			idx++
		}
	}
	if secs, ok := n.Data["sections"].([]interface{}); ok {
		for _, raw := range secs {
			sec, _ := raw.(map[string]interface{})
			if sec == nil {
				continue
			}
			rows, _ := sec["rows"].([]interface{})
			for _, rr := range rows {
				r, _ := rr.(map[string]interface{})
				if r == nil {
					continue
				}
				id, _ := r["id"].(string)
				title, _ := r["title"].(string)
				desc, _ := r["description"].(string)
				if branch, ok := matchOne(idx, id, strings.TrimSpace(title+" "+desc), nil); ok {
					return branch
				}
				idx++
			}
		}
	}
	return textRaw
}

// StartForMessage is invoked for every inbound WhatsApp message that should
// be routed through a flow. The session's configured flowID takes precedence;
// otherwise we fall back to the most recent enabled "message" trigger flow
// belonging to the same owner. Group messages are filtered upstream when the
// session disallows them.
func (e *FlowExecutor) StartForMessage(parent context.Context, sessionID, ownerID, flowID string, row MessageRow) {
	if e == nil {
		return
	}
	// Dedupe identical inbound deliveries (whatsmeow can redeliver the
	// same *events.Message during history sync / reconnect catch-up).
	// Manual triggers use unique IDs so they're not deduped here.
	if row.ID != "" && !strings.HasPrefix(row.ID, "manual_") {
		if e.markMsgSeen(sessionID, row.ChatJID, row.ID) {
			if e.log != nil {
				e.log.Debug("chat flow skipped: duplicate inbound", "sessionID", sessionID, "chat", row.ChatJID, "msgID", row.ID)
			}
			return
		}
	}
	// Coalesce: refuse a second concurrent run for the same chat. The
	// in-flight run will hit a chat_wait and persist state; subsequent
	// inbound messages then go through resumeWaitingChat normally.
	if !e.acquireChatRun(sessionID, row.ChatJID) {
		if e.log != nil {
			e.log.Info("chat flow skipped: another run already in-flight", "sessionID", sessionID, "chat", row.ChatJID)
		}
		return
	}
	released := false
	releaseOnce := func() {
		if !released {
			released = true
			e.releaseChatRun(sessionID, row.ChatJID)
		}
	}
	defer releaseOnce()
	resetWords := []string{"reiniciar", "reset", "menu", "voltar ao menu", "começar de novo", "comecar de novo"}
	bodyLower := strings.ToLower(strings.TrimSpace(row.Body))
	isReset := false
	for _, w := range resetWords {
		if bodyLower == w {
			isReset = true
			break
		}
	}
	if isReset {
		e.ResetChatThrottle(sessionID, row.ChatJID)
	} else {
		if pending, ok := e.popChatWait(sessionID, row.ChatJID); ok {
			// If a different explicit keyword flow is being started, keep the
			// pending state and let the requested flow run. Otherwise this inbound
			// reply is the answer to the waiting input/menu node.
			if flowID != "" && pending.FlowID != "" && flowID != pending.FlowID {
				e.setChatWait(sessionID, row.ChatJID, pending)
			} else {
				released = true
				go func() {
					defer e.releaseChatRun(sessionID, row.ChatJID)
					e.resumeWaitingChat(parent, sessionID, ownerID, pending, row)
				}()
				return
			}
		}
		if flowID == "" && e.chatRecentlyCompleted(sessionID, row.ChatJID) {
			if e.log != nil {
				e.log.Info("chat flow skipped: already completed for contact", "sessionID", sessionID, "chat", row.ChatJID)
			}
			return
		}
	}
	var (
		flow *FlowRow
		err  error
	)
	if flowID != "" {
		flow, err = e.store.Get(parent, flowID)
		if err != nil || flow == nil {
			if e.log != nil {
				e.log.Warn("chat flow lookup failed", "sessionID", sessionID, "flowID", flowID, "err", err)
			}
			e.emitFlowSkip(sessionID, "msg_"+row.ID, flowID, "flow_not_found", "fluxo vinculado não existe mais")
			return
		}
		if !flow.Enabled {
			if e.log != nil {
				e.log.Warn("chat flow disabled — skipping", "sessionID", sessionID, "flowID", flowID, "name", flow.Name)
			}
			e.emitFlowSkip(sessionID, "msg_"+row.ID, flowID, "flow_disabled", "habilite o fluxo \""+flow.Name+"\" no FlowBuilder")
			return
		}
	} else if ownerID != "" {
		flow, err = e.store.FindEnabledByTriggerForOwner(parent, "message", ownerID)
	} else {
		flow, err = e.store.FindEnabledByTrigger(parent, "message")
	}
	if err != nil || flow == nil || !flow.Enabled {
		return
	}
	if flowID == "" && flowKind(flow) == "voice" {
		return
	}
	if e.log != nil {
		e.log.Info("chat flow start", "sessionID", sessionID, "flowID", flow.ID, "name", flow.Name, "from", row.SenderJID, "chat", row.ChatJID)
	}
	runID := "msg_" + row.ID
	vars := map[string]interface{}{
		"message": chatMessageVars(sessionID, row),
		"call":    map[string]interface{}{"from": row.ChatJID, "session": sessionID, "id": runID},
		"flow":    map[string]interface{}{"id": flow.ID, "owner": ownerID},
		"vars":    map[string]interface{}{},
	}
	released = true
	go func() {
		defer e.releaseChatRun(sessionID, row.ChatJID)
		e.runFlow(parent, flow, runID, sessionID, vars, false)
	}()
}

func (e *FlowExecutor) resumeWaitingChat(parent context.Context, sessionID, ownerID string, pending *chatWaitState, row MessageRow) {
	if e == nil || pending == nil || pending.FlowID == "" || pending.NodeID == "" {
		return
	}
	flow, err := e.store.Get(parent, pending.FlowID)
	if err != nil || flow == nil || !flow.Enabled {
		if e.log != nil {
			e.log.Warn("chat wait resume skipped: flow unavailable", "sessionID", sessionID, "flowID", pending.FlowID, "err", err)
		}
		return
	}
	var graph FlowGraph
	if err := json.Unmarshal([]byte(flow.Graph), &graph); err != nil {
		if e.log != nil {
			e.log.Warn("chat wait resume skipped: graph parse failed", "sessionID", sessionID, "flowID", pending.FlowID, "err", err)
		}
		return
	}
	nodeByID := map[string]FlowNode{}
	for _, n := range graph.Nodes {
		nodeByID[n.ID] = n
	}
	n := nodeByID[pending.NodeID]
	vars := ensureFlowVars(copyVars(pending.Vars))
	runID := "msg_" + row.ID
	vars["message"] = chatMessageVars(sessionID, row)
	vars["call"] = map[string]interface{}{"from": row.ChatJID, "session": sessionID, "id": runID}
	vars["flow"] = map[string]interface{}{"id": flow.ID, "owner": ownerID}
	userText := strings.TrimSpace(row.Body)
	vm, _ := vars["vars"].(map[string]interface{})
	if pending.WaitKind == "menu" {
		branch := matchChatOptionBranch(n, userText)
		if pending.SaveAs != "" {
			vm[pending.SaveAs] = branch
		}
		vm[pending.NodeID+"_choice"] = branch
		vm[pending.NodeID+"_input"] = userText
		next := flowNextOf(graph, pending.NodeID, branch)
		if next == "" {
			next = flowNextOf(graph, pending.NodeID, "")
		}
		if next == "" {
			e.markChatCompleted(sessionID, row.ChatJID)
			return
		}
		if e.log != nil {
			e.log.Info("chat flow resume", "sessionID", sessionID, "flowID", flow.ID, "fromNode", pending.NodeID, "branch", branch, "next", next)
		}
		go e.runFlow(parent, flow, runID, sessionID, vars, false, next)
		return
	}
	saveAs := pending.SaveAs
	if saveAs == "" {
		saveAs, _ = n.Data["saveAs"].(string)
	}
	if saveAs == "" {
		saveAs = pending.NodeID + "_input"
	}
	vm[saveAs] = userText
	vm[pending.NodeID+"_input"] = userText
	next := flowNextOf(graph, pending.NodeID, "")
	if next == "" {
		e.markChatCompleted(sessionID, row.ChatJID)
		return
	}
	if e.log != nil {
		e.log.Info("chat flow resume", "sessionID", sessionID, "flowID", flow.ID, "fromNode", pending.NodeID, "next", next, "saveAs", saveAs)
	}
	go e.runFlow(parent, flow, runID, sessionID, vars, false, next)
}

// TestRun executes the flow synchronously in dry-run mode, returning the trace.
func (e *FlowExecutor) TestRun(ctx context.Context, flow *FlowRow, inputs map[string]interface{}) (string, error) {
	callID := "test_" + newRunID()
	vars := map[string]interface{}{
		"call": map[string]interface{}{"from": "+5500000000000", "session": "test", "id": callID},
		"vars": inputs,
	}
	return e.runFlow(ctx, flow, callID, "test", vars, true), nil
}

func (e *FlowExecutor) Abort(callID string) {
	e.mu.Lock()
	if c, ok := e.running[callID]; ok {
		c()
		delete(e.running, callID)
	}
	e.mu.Unlock()
}

// FindKeywordMatch scans the owner's enabled flows that declare keywords and
// returns the first one whose configured match rule fires for `body`. Body
// is normalized (lowercased + trimmed) before comparison. Returns nil when
// no flow matches — the caller falls back to the regular routing.
func (e *FlowExecutor) FindKeywordMatch(ctx context.Context, ownerID, body string) *FlowRow {
	if e == nil || e.store == nil || ownerID == "" {
		return nil
	}
	text := strings.ToLower(strings.TrimSpace(body))
	if text == "" {
		return nil
	}
	list, err := e.store.ListEnabledWithKeywordsForOwner(ctx, ownerID)
	if err != nil || len(list) == 0 {
		return nil
	}
	for i := range list {
		f := list[i]
		mode := f.KeywordMatch
		if mode == "" {
			mode = "contains"
		}
		for _, kw := range strings.Split(f.Keywords, ",") {
			kw = strings.TrimSpace(strings.ToLower(kw))
			if kw == "" {
				continue
			}
			matched := false
			switch mode {
			case "exact":
				matched = text == kw
			case "starts_with":
				matched = strings.HasPrefix(text, kw)
			default: // contains / any
				matched = strings.Contains(text, kw)
			}
			if matched {
				return &f
			}
		}
	}
	return nil
}

func (e *FlowExecutor) runFlow(parent context.Context, flow *FlowRow, callID, sessionID string, vars map[string]interface{}, dryRun bool, startAt ...string) string {
	var graph FlowGraph
	if err := json.Unmarshal([]byte(flow.Graph), &graph); err != nil {
		e.log.Error("flow graph parse failed", "flow", flow.ID, "err", err)
		return ""
	}
	if graph.StartNodeID == "" && len(graph.Nodes) > 0 {
		graph.StartNodeID = graph.Nodes[0].ID
	}
	vars = ensureFlowVars(vars)
	if _, ok := vars["flow"]; !ok && flow != nil {
		vars["flow"] = map[string]interface{}{"id": flow.ID, "owner": flow.OwnerID}
	}

	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	e.mu.Lock()
	e.running[callID] = cancel
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.running, callID)
		e.mu.Unlock()
	}()

	runID := newRunID()
	varJSON, _ := json.Marshal(vars)
	run := FlowRunRow{
		ID: runID, FlowID: flow.ID, CallID: callID, SessionID: sessionID,
		Status: "running", CurrentNode: graph.StartNodeID, Variables: string(varJSON),
		StartedAt: time.Now().Unix(),
	}
	if !dryRun {
		_ = e.store.InsertRun(parent, run)
	}

	trace := strings.Builder{}
	logEvent := func(nodeID, evType string, payload interface{}) {
		p, _ := json.Marshal(payload)
		fmt.Fprintf(&trace, "[%s] %s %s %s\n", time.Now().Format("15:04:05"), nodeID, evType, string(p))
		// Mirror every per-node event into the correlated tracer so the
		// /api/flows/trace endpoint shows the same path the user sees in
		// the FlowEditor run log — keyed by callID, not runID.
		level := "info"
		if strings.Contains(evType, "error") || strings.Contains(evType, "fail") || evType == "aborted" {
			level = "error"
		}
		e.trace(FlowTraceStep{
			CallID: callID, SessionID: sessionID,
			Level: level, Code: "node", Message: evType,
			Data: map[string]any{"nodeId": nodeID, "payload": json.RawMessage(p)},
		})
		if dryRun {
			return
		}
		_ = e.store.InsertEvent(parent, FlowEventRow{
			RunID: runID, Ts: time.Now().UnixMilli(), NodeID: nodeID,
			EventType: evType, Payload: string(p),
		})
	}

	nodeByID := map[string]FlowNode{}
	for _, n := range graph.Nodes {
		nodeByID[n.ID] = n
	}
	nextOf := func(nodeID, handle string) string {
		for _, ed := range graph.Edges {
			if ed.Source != nodeID {
				continue
			}
			if handle != "" && ed.SourceHandle != handle {
				continue
			}
			if handle == "" && ed.SourceHandle != "" && ed.SourceHandle != "out" {
				continue
			}
			return ed.Target
		}
		// Fallback: never let the URA silently stall because the user
		// connected the node with a generic edge while the runtime
		// requested a specific branch (e.g. an option key that nobody
		// wired). Return the first outgoing edge if any.
		if handle != "" {
			for _, ed := range graph.Edges {
				if ed.Source == nodeID {
					return ed.Target
				}
			}
		}
		return ""
	}

	current := graph.StartNodeID
	if len(startAt) > 0 && strings.TrimSpace(startAt[0]) != "" {
		current = strings.TrimSpace(startAt[0])
	}
	status := "completed"
	for steps := 0; current != "" && steps < 200; steps++ {
		select {
		case <-ctx.Done():
			status = "aborted"
			logEvent(current, "aborted", nil)
			goto done
		default:
		}
		n, ok := nodeByID[current]
		if !ok {
			status = "failed"
			logEvent(current, "error", map[string]string{"reason": "node not found"})
			break
		}
		logEvent(n.ID, "entered", map[string]string{"type": n.Type})
		next, branch, err := e.runNode(ctx, n, graph.Voice, vars, logEvent, dryRun, sessionID, callID)
		if branch == chatWaitBranch {
			status = "waiting"
			current = n.ID
			logEvent(n.ID, "chat_waiting", nil)
			goto done
		}
		if err != nil {
			status = "failed"
			logEvent(n.ID, "error", map[string]string{"error": err.Error()})
			break
		}
		if n.Type == "end" {
			break
		}
		if next == "" {
			next = nextOf(n.ID, branch)
		}
		current = next
	}
done:
	endTs := time.Now().Unix()
	if !dryRun {
		nv, _ := json.Marshal(vars)
		_ = e.store.UpdateRun(parent, runID, status, current, string(nv), &endTs)
	}
	// Mark per-contact completion so subsequent inbound messages do not
	// retrigger the chat flow from the top. Only meaningful for chat
	// flows that originate from a WhatsApp message (callID prefix "msg_").
	if status == "completed" && strings.HasPrefix(callID, "msg_") {
		if chat, _ := vars["message"].(map[string]interface{}); chat != nil {
			if jid, _ := chat["chat"].(string); jid != "" {
				e.markChatCompleted(sessionID, jid)
			}
		}
	}
	return trace.String()
}

// runNode returns (explicitNextID, branchHandle, error). When explicitNextID is
// empty the executor follows the edge with branchHandle (or default).
func (e *FlowExecutor) runNode(ctx context.Context, n FlowNode, flowVoice *FlowVoiceConfig, vars map[string]interface{}, logEv func(string, string, interface{}), dryRun bool, sessionID, callID string) (string, string, error) {
	switch n.Type {
	case "delay":
		secs := toFloat(n.Data["seconds"])
		if secs <= 0 {
			secs = 1
		}
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(time.Duration(secs * float64(time.Second))):
		}
		return "", "", nil

	case "condition":
		key, _ := n.Data["variable"].(string)
		op, _ := n.Data["operator"].(string)
		val, _ := n.Data["value"].(string)
		got := lookupVar(vars, key)
		ok := evalCondition(got, op, val)
		logEv(n.ID, "branched", map[string]interface{}{"variable": key, "got": got, "op": op, "value": val, "result": ok})
		if ok {
			return "", "true", nil
		}
		return "", "false", nil

	case "webhook":
		urlT, _ := n.Data["url"].(string)
		method, _ := n.Data["method"].(string)
		bodyT, _ := n.Data["body"].(string)
		saveAs, _ := n.Data["saveAs"].(string)
		if method == "" {
			method = "POST"
		}
		url := renderTemplate(urlT, vars)
		body := renderTemplate(bodyT, vars)
		if dryRun {
			logEv(n.ID, "webhook_skipped", map[string]string{"url": url, "method": method})
			return "", "", nil
		}
		req, _ := http.NewRequestWithContext(ctx, method, url, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return "", "", err
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
		logEv(n.ID, "webhook_done", map[string]interface{}{"status": resp.StatusCode, "len": len(out)})
		if saveAs != "" {
			if v, ok := vars["vars"].(map[string]interface{}); ok {
				v[saveAs] = string(out)
			}
		}
		return "", "", nil

	case "voice_menu":
		prompt, _ := n.Data["prompt"].(string)
		text := renderTemplate(prompt, vars)
		opts, _ := n.Data["options"].([]interface{})
		timeout := toFloat(n.Data["timeout"])
		if timeout <= 0 {
			// 5s era curto demais quando o cliente pensa antes de
			// responder; 8s casa com o comportamento de URAs reais.
			timeout = 8
		}
		audioCfg := AudioDetectConfig{
			SilenceMs:         int(toFloat(n.Data["silenceMs"])),
			MinSpeechMs:       int(toFloat(n.Data["minSpeechMs"])),
			SpeechThreshold:   toFloat(n.Data["speechThreshold"]),
			DisableDTMF:       toBool(n.Data["voiceOnly"]),
			DTMFWindowMs:      int(toFloat(n.Data["dtmfWindowMs"])),
			DTMFStableWindows: int(toFloat(n.Data["dtmfStableWindows"])),
			DTMFMinMagnitude:  toFloat(n.Data["dtmfMinMagnitude"]),
			DTMFDominance:     toFloat(n.Data["dtmfDominance"]),
		}
		logEv(n.ID, "audio_detect_config", map[string]string{
			"silenceMs":       fmt.Sprintf("%d", audioCfg.SilenceMs),
			"speechThreshold": fmt.Sprintf("%.4f", audioCfg.SpeechThreshold),
			"voiceOnly":       fmt.Sprintf("%t", audioCfg.DisableDTMF),
			"dtmfWindowMs":    fmt.Sprintf("%d", audioCfg.DTMFWindowMs),
		})
		voice, _ := n.Data["voice"].(string)
		voiceCfg := voiceConfigForNode(flowVoice, voice)
		sttOn := e.bridge != nil && e.bridge.STTConfigured()
		if !dryRun && e.bridge != nil && !sttOn {
			logEv(n.ID, "stt_missing", map[string]string{
				"hint": "configure WACALLS_STT_URL para reconhecer fala",
			})
		}
		// Estratégia: tocar o prompt, escutar, casar opção. Se não casou
		// nem entendeu, repete uma vez com mensagem curta antes de cair
		// no branch padrão. Isso é o que faz "o cliente fala e a URA
		// identifica o setor" funcionar de verdade na prática.
		attempts := []string{text, "Desculpa, não entendi. " + text}
		var transcript string
		var matchKey string
		anyTranscript := false
		for i, say := range attempts {
			if !dryRun && e.bridge != nil {
				if err := e.bridge.PlayTTSConfig(ctx, sessionID, callID, say, voiceCfg); err != nil {
					logEv(n.ID, "tts_error", map[string]string{"error": err.Error()})
				} else {
					logEv(n.ID, "tts_played", map[string]string{"text": say, "attempt": itoa(int64(i + 1))})
				}
			} else {
				logEv(n.ID, "tts_played", map[string]string{"text": say, "attempt": itoa(int64(i + 1))})
			}
			transcript = ""
			if !dryRun && e.bridge != nil {
				// Build a matchSet from option keys that look like single
				// DTMF chars so the URA reacts instantly to keypad input.
				matchSet := ""
				for _, raw := range opts {
					om, _ := raw.(map[string]interface{})
					k, _ := om["key"].(string)
					if len(k) == 1 && strings.ContainsRune("0123456789*#ABCD", rune(k[0])) {
						matchSet += k
					}
				}
				digit, t, err := e.bridge.RecordDTMFOrTranscribeConfig(ctx, sessionID, callID, int(timeout), matchSet, audioCfg)
				if err != nil {
					logEv(n.ID, "stt_error", map[string]string{"error": err.Error()})
				}
				if digit != "" {
					logEv(n.ID, "dtmf_detected", map[string]string{"digit": digit, "attempt": itoa(int64(i + 1))})
					if v, ok := vars["vars"].(map[string]interface{}); ok {
						v[n.ID+"_choice"] = digit
						v[n.ID+"_dtmf"] = digit
					}
					return "", digit, nil
				}
				transcript = t
				logEv(n.ID, "transcribed", map[string]string{"text": transcript, "attempt": itoa(int64(i + 1))})
			} else {
				select {
				case <-ctx.Done():
					return "", "", ctx.Err()
				case <-time.After(time.Duration(timeout * float64(time.Second))):
				}
			}
			matchKey = matchVoiceOption(transcript, opts)
			if transcript != "" {
				anyTranscript = true
			}
			if matchKey != "" {
				break
			}
		}
		if matchKey != "" {
			logEv(n.ID, "matched", map[string]string{"option": matchKey, "transcript": transcript})
			if v, ok := vars["vars"].(map[string]interface{}); ok {
				v[n.ID+"_choice"] = matchKey
				v[n.ID+"_transcript"] = transcript
			}
			return "", matchKey, nil
		}
		// Sem match após retries: prefere as saídas explícitas
		// __timeout / __invalid (configuradas no editor). Se nenhuma
		// estiver ligada, mantém o comportamento histórico (cai na
		// primeira opção como default) pra não silenciar a URA.
		fallbackBranch := "__invalid"
		if !anyTranscript {
			fallbackBranch = "__timeout"
		}
		logEv(n.ID, "matched_fallback", map[string]string{"branch": fallbackBranch, "transcript": transcript})
		if v, ok := vars["vars"].(map[string]interface{}); ok {
			v[n.ID+"_transcript"] = transcript
		}
		// O caller (nextOf) procura a aresta com sourceHandle == branch;
		// se o usuário não conectou __timeout/__invalid, cai na primeira
		// saída disponível, mantendo a URA fluindo.
		return "", fallbackBranch, nil

	case "message":
		prompt, _ := n.Data["prompt"].(string)
		text := renderTemplate(prompt, vars)
		voice, _ := n.Data["voice"].(string)
		voiceCfg := voiceConfigForNode(flowVoice, voice)
		if !dryRun && e.bridge != nil {
			if err := e.bridge.PlayTTSConfig(ctx, sessionID, callID, text, voiceCfg); err != nil {
				logEv(n.ID, "tts_error", map[string]string{"error": err.Error()})
				return "", "", nil
			}
		}
		logEv(n.ID, "tts_played", map[string]string{"text": text})
		return "", "", nil

	case "whatsapp_send":
		tmpl, _ := n.Data["template"].(string)
		toT, _ := n.Data["to"].(string)
		msg := renderTemplate(tmpl, vars)
		to := renderTemplate(toT, vars)
		if to == "" {
			// Default: send back to the caller's number.
			to = lookupVar(vars, "call.from")
		}
		// Tipo interativo: como o transporte atual só envia texto,
		// renderizamos os botões como lista numerada — o cliente ainda
		// recebe e pode responder com o número.
		if kind, _ := n.Data["msgKind"].(string); kind == "buttons" {
			if btns, ok := n.Data["buttons"].([]interface{}); ok && len(btns) > 0 {
				lines := []string{msg}
				for i, raw := range btns {
					b, _ := raw.(map[string]interface{})
					txt, _ := b["text"].(string)
					if txt == "" {
						continue
					}
					lines = append(lines, fmt.Sprintf("%d) %s", i+1, txt))
				}
				msg = strings.Join(lines, "\n")
			}
		}
		if dryRun || e.bridge == nil {
			logEv(n.ID, "whatsapp_skipped", map[string]string{"to": to, "message": msg})
			return "", "", nil
		}
		if err := e.bridge.SendWhatsAppText(ctx, sessionID, to, msg); err != nil {
			logEv(n.ID, "whatsapp_error", map[string]string{"error": err.Error(), "to": to})
			return "", "", nil
		}
		logEv(n.ID, "whatsapp_sent", map[string]string{"to": to, "message": msg})
		return "", "", nil

	case "ai_agent":
		agentID, _ := n.Data["agentId"].(string)
		logEv(n.ID, "agent_handoff", map[string]string{"agentId": agentID})
		// TODO: bridge to ElevenLabs Agents WebSocket.
		return "", "", nil

	case "transfer":
		dest, _ := n.Data["destination"].(string)
		queueID, _ := n.Data["queueId"].(string)
		if queueID == "" {
			queueID, _ = n.Data["queue"].(string)
		}
		userID, _ := n.Data["userId"].(string)
		if userID == "" {
			userID, _ = n.Data["operatorId"].(string)
		}
		queueID = strings.TrimSpace(renderTemplate(queueID, vars))
		userID = strings.TrimSpace(renderTemplate(userID, vars))
		prompt, _ := n.Data["prompt"].(string)
		if prompt == "" {
			prompt, _ = n.Data["message"].(string)
		}
		prompt = renderTemplate(prompt, vars)
		voiceID, _ := n.Data["voice"].(string)
		voiceCfg := voiceConfigForNode(flowVoice, voiceID)
		logEv(n.ID, "transfer_requested", map[string]string{
			"destination": dest, "queueId": queueID, "userId": userID,
		})
		if dryRun || e.bridge == nil {
			return "", "", nil
		}
		if queueID == "" && userID == "" {
			logEv(n.ID, "transfer_skipped", map[string]string{"reason": "no_destination"})
			return "", "", nil
		}
		if err := e.bridge.TransferCallToQueue(ctx, sessionID, callID, queueID, userID, prompt, voiceCfg); err != nil {
			logEv(n.ID, "transfer_error", map[string]string{"error": err.Error()})
			return "", "", nil
		}
		logEv(n.ID, "transfer_completed", map[string]string{"queueId": queueID, "userId": userID})
		return "", "", nil

	case "record_audio":
		secs := int(toFloat(n.Data["seconds"]))
		if secs <= 0 {
			secs = 5
		}
		saveAs, _ := n.Data["saveAs"].(string)
		if saveAs == "" {
			saveAs = "recording_url"
		}
		if dryRun || e.bridge == nil {
			logEv(n.ID, "record_skipped", map[string]int{"seconds": secs})
			return "", "", nil
		}
		url, err := e.bridge.RecordPeerAudio(ctx, sessionID, callID, secs)
		if err != nil {
			logEv(n.ID, "record_error", map[string]string{"error": err.Error()})
			return "", "", nil
		}
		if v, ok := vars["vars"].(map[string]interface{}); ok {
			v[saveAs] = url
		}
		logEv(n.ID, "record_saved", map[string]string{"url": url, "var": saveAs})
		return "", "", nil

	case "whatsapp_media":
		toT, _ := n.Data["to"].(string)
		urlT, _ := n.Data["mediaUrl"].(string)
		capT, _ := n.Data["caption"].(string)
		fileT, _ := n.Data["filename"].(string)
		kind, _ := n.Data["mediaKind"].(string)
		if kind == "" {
			kind = "image"
		}
		to := renderTemplate(toT, vars)
		if to == "" {
			to = lookupVar(vars, "call.from")
		}
		mediaURL := renderTemplate(urlT, vars)
		caption := renderTemplate(capT, vars)
		filename := renderTemplate(fileT, vars)
		if dryRun || e.bridge == nil {
			logEv(n.ID, "whatsapp_media_skipped", map[string]string{"to": to, "kind": kind, "url": mediaURL})
			return "", "", nil
		}
		if err := e.bridge.SendWhatsAppMedia(ctx, sessionID, to, kind, mediaURL, caption, filename); err != nil {
			logEv(n.ID, "whatsapp_media_error", map[string]string{"error": err.Error(), "to": to})
			return "", "", nil
		}
		logEv(n.ID, "whatsapp_media_sent", map[string]string{"to": to, "kind": kind, "url": mediaURL})
		return "", "", nil

	case "dtmf_capture":
		prompt, _ := n.Data["prompt"].(string)
		text := renderTemplate(prompt, vars)
		timeout := int(toFloat(n.Data["timeout"]))
		if timeout <= 0 {
			timeout = 6
		}
		maxDigits := int(toFloat(n.Data["maxDigits"]))
		if maxDigits <= 0 {
			maxDigits = 6
		}
		saveAs, _ := n.Data["saveAs"].(string)
		if saveAs == "" {
			saveAs = "digits"
		}
		voice, _ := n.Data["voice"].(string)
		voiceCfg := voiceConfigForNode(flowVoice, voice)
		if text != "" && !dryRun && e.bridge != nil {
			_ = e.bridge.PlayTTSConfig(ctx, sessionID, callID, text, voiceCfg)
		}
		digits := ""
		if !dryRun && e.bridge != nil {
			// Loop accumulating digits one at a time: each
			// RecordDTMFOrTranscribe call returns the instant the caller
			// presses a key, so the URA does not wait for the full
			// timeout to react. Falls back to STT digit extraction if
			// the caller speaks the number instead of pressing it.
			deadline := time.Now().Add(time.Duration(timeout) * time.Second)
			for len(digits) < maxDigits {
				remain := int(time.Until(deadline).Seconds())
				if remain <= 0 {
					break
				}
				digit, transcript, err := e.bridge.RecordDTMFOrTranscribe(ctx, sessionID, callID, remain, "0123456789*#")
				if err != nil {
					logEv(n.ID, "dtmf_error", map[string]string{"error": err.Error()})
					break
				}
				if digit != "" {
					digits += digit
					logEv(n.ID, "dtmf_digit", map[string]string{"digit": digit, "buffer": digits})
					continue
				}
				// No DTMF and (maybe) a spoken phrase: extract digits and stop.
				if transcript != "" {
					digits += extractDigits(transcript, maxDigits-len(digits))
				}
				break
			}
			logEv(n.ID, "dtmf_captured", map[string]string{"digits": digits})
		} else {
			logEv(n.ID, "dtmf_skipped", map[string]int{"timeout": timeout})
		}
		if v, ok := vars["vars"].(map[string]interface{}); ok {
			v[saveAs] = digits
		}
		return "", "", nil

	case "set_variable":
		key, _ := n.Data["variable"].(string)
		valT, _ := n.Data["value"].(string)
		val := renderTemplate(valT, vars)
		if key != "" {
			if v, ok := vars["vars"].(map[string]interface{}); ok {
				v[key] = val
			}
		}
		logEv(n.ID, "var_set", map[string]string{"variable": key, "value": val})
		return "", "", nil

	case "end":
		mode, _ := n.Data["mode"].(string)
		final, _ := n.Data["finalText"].(string)
		if final == "" {
			final, _ = n.Data["prompt"].(string)
		}
		final = renderTemplate(final, vars)
		if final != "" && !dryRun && e.bridge != nil {
			voice, _ := n.Data["voice"].(string)
			voiceCfg := voiceConfigForNode(flowVoice, voice)
			if err := e.bridge.PlayTTSConfig(ctx, sessionID, callID, final, voiceCfg); err != nil {
				logEv(n.ID, "tts_error", map[string]string{"error": err.Error()})
			} else {
				logEv(n.ID, "tts_played", map[string]string{"text": final})
			}
			mode = "spoken"
		}
		logEv(n.ID, "completed", map[string]string{"mode": mode})
		return "", "", nil
	}
	// Chat-flow + Instagram nodes live in flowexec_chat.go to keep this
	// switch readable. Unknown types still surface as an explicit error.
	if strings.HasPrefix(n.Type, "chat_") || strings.HasPrefix(n.Type, "ig_") {
		return e.runChatNode(ctx, n, flowVoice, vars, logEv, dryRun, sessionID, callID)
	}
	return "", "", errors.New("unknown node type: " + n.Type)
}

// extractDigits pulls digit characters (and common spoken digit words in
// Portuguese/English) out of a transcript. Stops after `max` digits when > 0.
func extractDigits(transcript string, max int) string {
	t := strings.ToLower(transcript)
	words := map[string]string{
		"zero": "0", "um": "1", "uma": "1", "dois": "2", "duas": "2", "tres": "3", "três": "3",
		"quatro": "4", "cinco": "5", "seis": "6", "meia": "6", "sete": "7", "oito": "8", "nove": "9",
		"one": "1", "two": "2", "three": "3", "four": "4", "five": "5", "six": "6", "seven": "7",
		"eight": "8", "nine": "9",
	}
	for w, d := range words {
		t = strings.ReplaceAll(t, w, " "+d+" ")
	}
	var b strings.Builder
	for _, r := range t {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
			if max > 0 && b.Len() >= max {
				break
			}
		}
	}
	return b.String()
}

func voiceConfigForNode(flowVoice *FlowVoiceConfig, nodeVoice string) *FlowVoiceConfig {
	nodeVoice = strings.TrimSpace(nodeVoice)
	if flowVoice == nil {
		if nodeVoice == "" {
			return nil
		}
		return &FlowVoiceConfig{Provider: "piper", VoiceID: nodeVoice}
	}
	cfg := *flowVoice
	if nodeVoice != "" {
		cfg.VoiceID = nodeVoice
	}
	if strings.TrimSpace(cfg.Provider) == "" && strings.TrimSpace(cfg.VoiceID) != "" {
		cfg.Provider = "piper"
	}
	return &cfg
}

// matchVoiceOption returns the option key whose phrases best match the
// transcript, or the empty string when no option matches.
//
// Improvements relevant to real STT output (Whisper/Piper bridge):
//   - Diacritics are stripped ("são" -> "sao").
//   - Whole-word match (avoids "sim" matching inside "assim").
//   - Auto-synonyms: index-based ordinals ("um", "primeira opção", "número 1")
//     and tokens extracted from the label/key.
//   - If the transcript is just a digit (or word-digit) like "dois", it
//     selects the corresponding Nth option even if the option didn't list
//     that digit as a synonym.
//   - Longest match wins; earliest occurrence breaks ties.
func matchVoiceOption(transcript string, opts []interface{}) string {
	t := normalizeSpeech(transcript)
	if t == "" {
		return ""
	}
	// Digit-only fallback: "2", "dois", "opcao 2", "numero 2".
	if digit := firstDigit(t); digit > 0 && digit <= len(opts) {
		if o, ok := opts[digit-1].(map[string]interface{}); ok {
			if k, _ := o["key"].(string); k != "" {
				return k
			}
		}
	}
	type cand struct {
		key string
		ln  int
		pos int
	}
	best := cand{pos: 1 << 30}
	for i, raw := range opts {
		o, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		key, _ := o["key"].(string)
		if key == "" {
			continue
		}
		phrases := []string{}
		for _, field := range []string{"synonyms", "phrases"} {
			if list, ok := o[field].([]interface{}); ok {
				for _, p := range list {
					if s, ok := p.(string); ok && s != "" {
						phrases = append(phrases, s)
					}
				}
			}
		}
		if label, _ := o["label"].(string); label != "" {
			phrases = append(phrases, label)
		}
		phrases = append(phrases, key)
		// Tokens individuais do label/key ajudam quando o cliente diz só
		// uma palavra ("segunda via" -> também casa "segunda").
		for _, src := range []string{key} {
			for _, tok := range strings.Fields(normalizeSpeech(src)) {
				if len(tok) >= 4 {
					phrases = append(phrases, tok)
				}
			}
		}
		// Sinônimos numéricos automáticos para a posição (1-based).
		phrases = append(phrases, ordinalSynonyms(i+1)...)

		for _, p := range phrases {
			lp := normalizeSpeech(p)
			if lp == "" {
				continue
			}
			pos := wordIndex(t, lp)
			if pos < 0 {
				continue
			}
			if len(lp) > best.ln || (len(lp) == best.ln && pos < best.pos) {
				best = cand{key: key, ln: len(lp), pos: pos}
			}
		}
	}
	return best.key
}

// normalizeSpeech lowercases, strips diacritics and replaces punctuation with
// spaces so word matching is reliable across STT engines.
func normalizeSpeech(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	repl := map[rune]rune{
		'á': 'a', 'à': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a',
		'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
		'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
		'ó': 'o', 'ò': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
		'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
		'ç': 'c', 'ñ': 'n',
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if n, ok := repl[r]; ok {
			b.WriteRune(n)
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	// Collapse multiple spaces.
	out := strings.Join(strings.Fields(b.String()), " ")
	return out
}

// wordIndex finds `needle` in `hay` matching whole words (space-delimited).
// Returns the byte offset, or -1 when not found.
func wordIndex(hay, needle string) int {
	if needle == "" {
		return -1
	}
	padded := " " + hay + " "
	idx := strings.Index(padded, " "+needle+" ")
	if idx < 0 {
		return -1
	}
	return idx
}

// firstDigit returns the first digit (1-9) found in the normalized transcript,
// recognising spoken numbers in Portuguese/English. Returns 0 when none.
func firstDigit(t string) int {
	words := map[string]int{
		"um": 1, "uma": 1, "primeira": 1, "primeiro": 1, "one": 1,
		"dois": 2, "duas": 2, "segunda": 2, "segundo": 2, "two": 2,
		"tres": 3, "terceira": 3, "terceiro": 3, "three": 3,
		"quatro": 4, "quarta": 4, "quarto": 4, "four": 4,
		"cinco": 5, "quinta": 5, "quinto": 5, "five": 5,
		"seis": 6, "sexta": 6, "sexto": 6, "six": 6,
		"sete": 7, "setima": 7, "setimo": 7, "seven": 7,
		"oito": 8, "oitava": 8, "oitavo": 8, "eight": 8,
		"nove": 9, "nona": 9, "nono": 9, "nine": 9,
	}
	for _, w := range strings.Fields(t) {
		if d, ok := words[w]; ok {
			return d
		}
	}
	for _, r := range t {
		if r >= '1' && r <= '9' {
			return int(r - '0')
		}
	}
	return 0
}

// ordinalSynonyms expands "1" into the spoken variants a caller might use to
// pick the Nth option. Kept small on purpose; the substring matcher does the
// rest of the work.
func ordinalSynonyms(n int) []string {
	if n <= 0 || n > 9 {
		return nil
	}
	digit := string(rune('0' + n))
	words := []string{"", "um", "dois", "tres", "quatro", "cinco", "seis", "sete", "oito", "nove"}
	ords := []string{"", "primeira", "segunda", "terceira", "quarta", "quinta", "sexta", "setima", "oitava", "nona"}
	w := words[n]
	o := ords[n]
	return []string{
		digit, w, o,
		"opcao " + digit, "opcao " + w,
		"numero " + digit, "numero " + w,
		o + " opcao",
	}
}

// ---- helpers ------------------------------------------------------------

func toFloat(v interface{}) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case string:
		var f float64
		fmt.Sscanf(x, "%f", &f)
		return f
	}
	return 0
}

func toBool(v interface{}) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		s := strings.TrimSpace(strings.ToLower(x))
		return s == "true" || s == "1" || s == "yes" || s == "sim" || s == "on"
	case float64:
		return x != 0
	case int:
		return x != 0
	}
	return false
}

func lookupVar(vars map[string]interface{}, path string) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(path, ".")
	var cur interface{} = vars
	for _, p := range parts {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return ""
		}
		cur = m[p]
	}
	if cur == nil {
		return ""
	}
	return fmt.Sprintf("%v", cur)
}

func evalCondition(got, op, val string) bool {
	switch op {
	case "eq", "==", "":
		return got == val
	case "neq", "!=":
		return got != val
	case "contains":
		return strings.Contains(strings.ToLower(got), strings.ToLower(val))
	case "starts_with":
		return strings.HasPrefix(got, val)
	case "empty":
		return got == ""
	case "not_empty":
		return got != ""
	}
	return false
}

func renderTemplate(tmpl string, vars map[string]interface{}) string {
	if tmpl == "" || !strings.Contains(tmpl, "{{") {
		return tmpl
	}
	t, err := template.New("f").Option("missingkey=zero").Parse(tmpl)
	if err != nil {
		return tmpl
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return tmpl
	}
	return buf.String()
}
