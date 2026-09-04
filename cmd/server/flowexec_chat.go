package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

// runChatNode implements the chat-flow and Instagram-flow node types. It is
// invoked from runNode's default branch (see flowexec.go) so the main switch
// stays readable. All "send" actions reuse the existing FlowBridge; nodes
// that require waiting for an inbound message (chat_menu, chat_input) are
// not yet wired to a message-driven runtime and fall through to the default
// branch with a "waiting_disabled" warning so the user can see what happened
// in the trace.
func (e *FlowExecutor) runChatNode(
	ctx context.Context,
	n FlowNode,
	flowVoice *FlowVoiceConfig,
	vars map[string]interface{},
	logEv func(string, string, interface{}),
	dryRun bool,
	sessionID, callID string,
) (string, string, error) {
	get := func(k string) string {
		v, _ := n.Data[k].(string)
		return renderTemplate(v, vars)
	}
	setVar := func(k string, v interface{}) {
		if k == "" {
			return
		}
		if m, ok := vars["vars"].(map[string]interface{}); ok {
			m[k] = v
		}
	}
	resolveTo := func() string {
		to := get("to")
		if to == "" {
			// Chatbot replies must go to the chat JID. On 1:1 chats the
			// sender can be empty depending on the WhatsApp event source; on
			// groups, using sender would DM the participant instead of replying
			// in the group. Prefer message.chat and keep message.from/call.from
			// only as legacy fallbacks.
			to = lookupVar(vars, "message.chat")
		}
		if to == "" {
			to = lookupVar(vars, "message.from")
		}
		if to == "" {
			to = lookupVar(vars, "call.from")
		}
		return to
	}

	switch n.Type {
	// ---- Conteúdo / Texto ------------------------------------------------
	case "chat_text", "chat_content":
		text := get("text")
		if text == "" {
			text = get("prompt")
		}
		if text == "" {
			text = get("template")
		}
		to := resolveTo()
		mediaURL := get("mediaUrl")
		kind, _ := n.Data["mediaKind"].(string)
		if mediaURL != "" {
			if dryRun || e.bridge == nil {
				logEv(n.ID, "chat_media_skipped", map[string]string{"to": to, "url": mediaURL})
				return "", "", nil
			}
			if kind == "" {
				kind = "image"
			}
			if err := e.bridge.SendWhatsAppMedia(ctx, sessionID, to, kind, mediaURL, text, get("filename")); err != nil {
				logEv(n.ID, "chat_media_error", map[string]string{"error": err.Error()})
				return "", "", nil
			}
			logEv(n.ID, "chat_media_sent", map[string]string{"to": to, "kind": kind})
			return "", "", nil
		}
		if text == "" {
			logEv(n.ID, "chat_text_empty", nil)
			return "", "", nil
		}
		if dryRun || e.bridge == nil {
			logEv(n.ID, "chat_text_skipped", map[string]string{"to": to, "text": text})
			return "", "", nil
		}
		if err := e.bridge.SendWhatsAppText(ctx, sessionID, to, text); err != nil {
			logEv(n.ID, "chat_text_error", map[string]string{"error": err.Error()})
			return "", "", nil
		}
		logEv(n.ID, "chat_text_sent", map[string]string{"to": to, "len": itoa(int64(len(text)))})
		// Small pacing pause so consecutive sends end up with distinct
		// server timestamps and arrive in the correct visual order on the
		// recipient's WhatsApp (whatsmeow ack timestamps are second-precision).
		time.Sleep(900 * time.Millisecond)
		return "", "", nil

	// ---- Interação -------------------------------------------------------
	case "chat_menu", "chat_msg_api", "chat_input":
		// Native interactive rendering: when the node has up to 3 options
		// and no "sections", send WhatsApp quick-reply buttons. When it has
		// sections or more than 3 options, send a native list. Both fall
		// back to a numbered text message on send error or when "renderAs"
		// is explicitly "text".
		body := get("prompt")
		if body == "" {
			body = get("text")
		}
		footer, _ := n.Data["footer"].(string)
		footer = renderTemplate(footer, vars)
		renderAs, _ := n.Data["renderAs"].(string)
		to := resolveTo()

		// Collect button options.
		var buttons []FlowButton
		if opts, ok := n.Data["options"].([]interface{}); ok {
			for i, raw := range opts {
				o, _ := raw.(map[string]interface{})
				if o == nil {
					continue
				}
				label, _ := o["label"].(string)
				if label == "" {
					label, _ = o["text"].(string)
				}
				key, _ := o["key"].(string)
				if label == "" {
					label = key
				}
				if label == "" {
					continue
				}
				id := key
				if id == "" {
					id = itoa(int64(i + 1))
				}
				buttons = append(buttons, FlowButton{ID: id, Title: renderTemplate(label, vars)})
			}
		}
		// Collect list sections.
		var sections []FlowListSection
		if secs, ok := n.Data["sections"].([]interface{}); ok {
			for _, raw := range secs {
				sec, _ := raw.(map[string]interface{})
				if sec == nil {
					continue
				}
				ls := FlowListSection{}
				ls.Title, _ = sec["title"].(string)
				rows, _ := sec["rows"].([]interface{})
				for _, rr := range rows {
					r, _ := rr.(map[string]interface{})
					if r == nil {
						continue
					}
					rt, _ := r["title"].(string)
					rd, _ := r["description"].(string)
					rid, _ := r["id"].(string)
					if rt == "" {
						continue
					}
					ls.Rows = append(ls.Rows, FlowListRow{ID: rid, Title: renderTemplate(rt, vars), Description: renderTemplate(rd, vars)})
				}
				if len(ls.Rows) > 0 {
					sections = append(sections, ls)
				}
			}
		}

		if body == "" && len(buttons) == 0 && len(sections) == 0 {
			logEv(n.ID, "chat_menu_empty", nil)
			return "", "", nil
		}
		if body == "" {
			body = "—"
		}

		if dryRun || e.bridge == nil {
			logEv(n.ID, "chat_menu_skipped", map[string]string{"to": to, "text": body})
			return "", "", nil
		}

		waitKind := "menu"
		if n.Type == "chat_input" {
			waitKind = "input"
			// Input nodes only ask the prompt and then pause until the next
			// inbound message. They must not immediately continue to the next
			// node, otherwise variables like saveAs are always empty.
			if err := e.bridge.SendWhatsAppText(ctx, sessionID, to, body); err != nil {
				logEv(n.ID, "chat_input_error", map[string]string{"error": err.Error()})
				return "", "", nil
			}
			logEv(n.ID, "chat_input_sent", map[string]string{"to": to})
			saveAs, _ := n.Data["saveAs"].(string)
			e.setChatWait(sessionID, lookupVar(vars, "message.chat"), &chatWaitState{
				FlowID: lookupVar(vars, "flow.id"), OwnerID: lookupVar(vars, "flow.owner"),
				NodeID: n.ID, WaitKind: waitKind, SaveAs: saveAs, Vars: copyVars(vars),
			})
			time.Sleep(900 * time.Millisecond)
			return "", chatWaitBranch, nil
		}

		// Decide rendering path.
		useButtons := renderAs != "text" && renderAs != "list" && len(sections) == 0 && len(buttons) > 0 && len(buttons) <= 3
		useList := renderAs != "text" && renderAs != "buttons" && (len(sections) > 0 || len(buttons) > 3)

		var sendErr error
		switch {
		case useButtons:
			sendErr = e.bridge.SendWhatsAppButtons(ctx, sessionID, to, body, footer, buttons)
			logEv(n.ID, "chat_menu_sent", map[string]string{"to": to, "render": "buttons", "n": itoa(int64(len(buttons)))})
		case useList:
			// If only flat buttons (>3) and no sections, fold them into a single section.
			if len(sections) == 0 {
				rows := make([]FlowListRow, 0, len(buttons))
				for _, b2 := range buttons {
					rows = append(rows, FlowListRow{ID: b2.ID, Title: b2.Title})
				}
				sections = []FlowListSection{{Title: "Opções", Rows: rows}}
			}
			btnText, _ := n.Data["buttonText"].(string)
			sendErr = e.bridge.SendWhatsAppList(ctx, sessionID, to, body, footer, renderTemplate(btnText, vars), sections)
			logEv(n.ID, "chat_menu_sent", map[string]string{"to": to, "render": "list", "n": itoa(int64(len(sections)))})
		default:
			// Plain text with numbered options.
			lines := []string{body}
			for i, bt := range buttons {
				lines = append(lines, fmt.Sprintf("[ %d ] %s", i+1, bt.Title))
			}
			n2 := 0
			for _, sec := range sections {
				if sec.Title != "" {
					lines = append(lines, "", "*"+sec.Title+"*")
				}
				for _, r := range sec.Rows {
					n2++
					line := fmt.Sprintf("[ %d ] %s", n2, r.Title)
					if r.Description != "" {
						line += " — " + r.Description
					}
					lines = append(lines, line)
				}
			}
			if footer != "" {
				lines = append(lines, "", footer)
			}
			sendErr = e.bridge.SendWhatsAppText(ctx, sessionID, to, strings.Join(lines, "\n"))
			logEv(n.ID, "chat_menu_sent", map[string]string{"to": to, "render": "text"})
		}
		if sendErr != nil {
			logEv(n.ID, "chat_menu_error", map[string]string{"error": sendErr.Error()})
		}
		// Pause the runtime here. The next inbound text/button/list reply will
		// resume from the edge matching the chosen option.
		saveAs, _ := n.Data["saveAs"].(string)
		e.setChatWait(sessionID, lookupVar(vars, "message.chat"), &chatWaitState{
			FlowID: lookupVar(vars, "flow.id"), OwnerID: lookupVar(vars, "flow.owner"),
			NodeID: n.ID, WaitKind: waitKind, SaveAs: saveAs, Vars: copyVars(vars),
		})
		// Pacing — same rationale as chat_text.
		time.Sleep(900 * time.Millisecond)
		return "", chatWaitBranch, nil

	case "chat_interval":
		secs := toFloat(n.Data["seconds"])
		if secs <= 0 {
			secs = 1
		}
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(time.Duration(secs * float64(time.Second))):
		}
		logEv(n.ID, "chat_interval_done", map[string]float64{"seconds": secs})
		return "", "", nil

	// ---- Lógica ----------------------------------------------------------
	case "chat_random":
		// Pick a weighted random outgoing edge from the graph.
		opts, _ := n.Data["options"].([]interface{})
		if len(opts) == 0 {
			if rand.Intn(2) == 0 {
				return "", "a", nil
			}
			return "", "b", nil
		}
		type weighted struct {
			key string
			w   float64
		}
		ws := make([]weighted, 0, len(opts))
		total := 0.0
		for _, raw := range opts {
			o, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			k, _ := o["key"].(string)
			w := 1.0
			switch v := o["weight"].(type) {
			case float64:
				w = v
			case int:
				w = float64(v)
			}
			if w < 0 {
				w = 0
			}
			ws = append(ws, weighted{k, w})
			total += w
		}
		if total <= 0 {
			idx := rand.Intn(len(ws))
			logEv(n.ID, "chat_random_pick", map[string]string{"branch": ws[idx].key})
			return "", ws[idx].key, nil
		}
		r := rand.Float64() * total
		acc := 0.0
		key := ws[len(ws)-1].key
		for _, w := range ws {
			acc += w.w
			if r <= acc {
				key = w.key
				break
			}
		}
		logEv(n.ID, "chat_random_pick", map[string]string{"branch": key})
		return "", key, nil

	case "chat_if_else":
		// New format: array of {variable, operator, value} combined by logic (and/or).
		conds, _ := n.Data["conditions"].([]interface{})
		logic, _ := n.Data["logic"].(string)
		if logic == "" {
			logic = "and"
		}
		result := true
		if len(conds) == 0 {
			// Legacy single-condition fallback.
			key, _ := n.Data["variable"].(string)
			op, _ := n.Data["operator"].(string)
			val, _ := n.Data["value"].(string)
			result = evalCondition(lookupVar(vars, key), op, val)
		} else {
			if logic == "or" {
				result = false
			}
			for _, raw := range conds {
				c, ok := raw.(map[string]interface{})
				if !ok {
					continue
				}
				k, _ := c["variable"].(string)
				op, _ := c["operator"].(string)
				val, _ := c["value"].(string)
				one := evalCondition(lookupVar(vars, k), op, val)
				if logic == "and" {
					result = result && one
				} else {
					result = result || one
				}
			}
		}
		logEv(n.ID, "chat_if_else", map[string]interface{}{"result": result, "logic": logic, "n": len(conds)})
		if result {
			return "", "true", nil
		}
		return "", "false", nil

	// ---- Sistema ---------------------------------------------------------
	case "chat_queue":
		q := get("queueId")
		if q == "" {
			q, _ = n.Data["queue"].(string)
		}
		setVar("queue", q)
		logEv(n.ID, "chat_queue_set", map[string]string{"queue": q})
		return "", "", nil

	case "chat_tag_add":
		t := get("tag")
		setVar("last_tag_added", t)
		logEv(n.ID, "chat_tag_added", map[string]string{"tag": t})
		return "", "", nil

	case "chat_tag_remove":
		t := get("tag")
		setVar("last_tag_removed", t)
		logEv(n.ID, "chat_tag_removed", map[string]string{"tag": t})
		return "", "", nil

	case "chat_switch_flow":
		target := get("flowId")
		logEv(n.ID, "chat_switch_flow", map[string]string{"target": target})
		return "", "", nil

	case "chat_attendant":
		dest := get("destination")
		if dest == "" {
			dest = get("queue")
		}
		logEv(n.ID, "chat_handoff", map[string]string{"destination": dest})
		return "", "", nil

	// ---- Integrações -----------------------------------------------------
	case "chat_http":
		method, _ := n.Data["method"].(string)
		if method == "" {
			method = "GET"
		}
		url := get("url")
		body := get("body")
		saveAs, _ := n.Data["saveAs"].(string)
		if saveAs == "" {
			saveAs = "apiResponse"
		}
		if dryRun {
			logEv(n.ID, "chat_http_skipped", map[string]string{"url": url, "method": method})
			return "", "", nil
		}
		req, _ := http.NewRequestWithContext(ctx, method, url, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		// Apply custom headers (JSON string) when provided.
		if hdrRaw, _ := n.Data["headers"].(string); hdrRaw != "" {
			var hdrs map[string]string
			if json.Unmarshal([]byte(hdrRaw), &hdrs) == nil {
				for k, v := range hdrs {
					req.Header.Set(k, v)
				}
			}
		}
		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			logEv(n.ID, "chat_http_error", map[string]string{"error": err.Error()})
			return "", "", nil
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		logEv(n.ID, "chat_http_done", map[string]interface{}{"status": resp.StatusCode, "len": len(out)})
		// Save full response (JSON-decoded when possible).
		var parsed interface{}
		if json.Unmarshal(out, &parsed) == nil {
			setVar(saveAs, parsed)
		} else {
			setVar(saveAs, string(out))
			parsed = string(out)
		}
		// Apply responseMap → extract values into named variables.
		if maps, ok := n.Data["responseMap"].([]interface{}); ok {
			for _, raw := range maps {
				m, _ := raw.(map[string]interface{})
				if m == nil {
					continue
				}
				name, _ := m["variable"].(string)
				path, _ := m["path"].(string)
				if name == "" || path == "" {
					continue
				}
				setVar(name, jsonPath(parsed, path))
			}
		}
		return "", "", nil

	case "chat_variable":
		key, _ := n.Data["variable"].(string)
		val := get("value")
		setVar(key, val)
		logEv(n.ID, "chat_var_set", map[string]string{"variable": key, "value": val})
		return "", "", nil

	case "chat_ai_agent":
		agentID, _ := n.Data["agentId"].(string)
		logEv(n.ID, "chat_ai_handoff", map[string]string{"agentId": agentID})
		return "", "", nil

	case "chat_n8n":
		// n8n webhook integration: POST payload to the configured webhook URL.
		url := get("url")
		if url == "" {
			logEv(n.ID, "chat_n8n_skipped", map[string]string{"reason": "missing webhook url"})
			return "", "", nil
		}
		method, _ := n.Data["method"].(string)
		if method == "" {
			method = "POST"
		}
		body := get("body")
		if body == "" {
			b, _ := json.Marshal(map[string]interface{}{
				"from":    lookupVar(vars, "message.from"),
				"chat":    lookupVar(vars, "message.chat"),
				"vars":    vars["vars"],
				"message": vars["message"],
			})
			body = string(b)
		}
		if dryRun {
			logEv(n.ID, "chat_n8n_skipped", map[string]string{"url": url, "reason": "dryRun"})
			return "", "", nil
		}
		req, _ := http.NewRequestWithContext(ctx, strings.ToUpper(method), url, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		if auth, ok := n.Data["authHeader"].(string); ok && auth != "" {
			req.Header.Set("Authorization", renderTemplate(auth, vars))
		}
		if hdrs, ok := n.Data["headers"].([]interface{}); ok {
			for _, raw := range hdrs {
				m, _ := raw.(map[string]interface{})
				if m == nil {
					continue
				}
				k, _ := m["key"].(string)
				v, _ := m["value"].(string)
				if k != "" {
					req.Header.Set(k, renderTemplate(v, vars))
				}
			}
		}
		client := &http.Client{Timeout: 20 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			logEv(n.ID, "chat_n8n_error", map[string]string{"error": err.Error()})
			return "", "", nil
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		logEv(n.ID, "chat_n8n_done", map[string]interface{}{"status": resp.StatusCode, "bytes": len(respBody)})
		// Optional: store response into a variable
		if outVar, ok := n.Data["responseVariable"].(string); ok && outVar != "" {
			setVar(outVar, string(respBody))
		}
		// Optional: map JSON fields to variables
		if maps, ok := n.Data["responseMap"].([]interface{}); ok && len(maps) > 0 {
			var parsed interface{}
			if err := json.Unmarshal(respBody, &parsed); err == nil {
				for _, raw := range maps {
					m, _ := raw.(map[string]interface{})
					if m == nil {
						continue
					}
					name, _ := m["variable"].(string)
					path, _ := m["path"].(string)
					if name == "" || path == "" {
						continue
					}
					setVar(name, fmt.Sprint(jsonPath(parsed, path)))
				}
			}
		}
		return "", "", nil

	// ---- Instagram (placeholders honestos) ------------------------------
	case "ig_trigger_comment", "ig_reply_comment", "ig_like_comment",
		"ig_is_follower", "ig_send_dm", "ig_send_reward":
		logEv(n.ID, "ig_skipped", map[string]string{
			"type": n.Type,
			"note": "integração Instagram ainda não conectada — nó passa em branco",
		})
		if n.Type == "ig_is_follower" {
			return "", "true", nil
		}
		return "", "", nil
	}

	logEv(n.ID, "chat_unknown", map[string]string{"type": n.Type})
	return "", "", nil
}

// jsonPath resolves a dotted path (e.g. "data.user.id" or "items.0.name") against
// a JSON-decoded value. Returns nil when any segment is missing.
func jsonPath(root interface{}, path string) interface{} {
	if path == "" {
		return root
	}
	cur := root
	start := 0
	for i := 0; i <= len(path); i++ {
		if i == len(path) || path[i] == '.' {
			seg := path[start:i]
			start = i + 1
			if seg == "" {
				continue
			}
			switch v := cur.(type) {
			case map[string]interface{}:
				cur = v[seg]
			case []interface{}:
				idx := 0
				for _, ch := range seg {
					if ch < '0' || ch > '9' {
						return nil
					}
					idx = idx*10 + int(ch-'0')
				}
				if idx < 0 || idx >= len(v) {
					return nil
				}
				cur = v[idx]
			default:
				return nil
			}
		}
	}
	return cur
}
