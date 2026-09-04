package main

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Default SLA targets when the caller does not send its own.
const (
	defaultFirstResponseTargetMs int64 = 5 * 60 * 1000       // 5 min
	defaultResolutionTargetMs    int64 = 4 * 60 * 60 * 1000  // 4 h
)

type slaMetrics struct {
	Conversations       int   `json:"conversations"`
	Answered            int   `json:"answered"`
	Resolved            int   `json:"resolved"`
	Pending             int   `json:"pending"`
	AvgFirstResponseMs  int64 `json:"avgFirstResponseMs"`
	MaxFirstResponseMs  int64 `json:"maxFirstResponseMs"`
	AvgResolutionMs     int64 `json:"avgResolutionMs"`
	MaxResolutionMs     int64 `json:"maxResolutionMs"`
	FirstResponseBreach int   `json:"firstResponseBreach"`
	ResolutionBreach    int   `json:"resolutionBreach"`
	// Percentage (0-100) of answered conversations inside the response target.
	Compliance int `json:"compliance"`
	// CSAT: number of survey answers and the average score (1=Bom .. 3=Péssimo).
	RatingCount int     `json:"ratingCount"`
	RatingAvg   float64 `json:"ratingAvg"`
}

type slaGroup struct {
	ID      string     `json:"id"`
	Label   string     `json:"label"`
	Metrics slaMetrics `json:"metrics"`
}

type slaAlert struct {
	SessionID       string `json:"sessionId"`
	SessionName     string `json:"sessionName,omitempty"`
	ChatJID         string `json:"chatJid"`
	Name            string `json:"name,omitempty"`
	QueueID         string `json:"queueId,omitempty"`
	QueueName       string `json:"queueName,omitempty"`
	Kind            string `json:"kind"` // first-response | resolution | unanswered
	FirstResponseMs int64  `json:"firstResponseMs,omitempty"`
	ResolutionMs    int64  `json:"resolutionMs,omitempty"`
	WaitingMs       int64  `json:"waitingMs,omitempty"`
	Ts              int64  `json:"ts"`
}

type slaReport struct {
	From                  int64      `json:"from"`
	To                    int64      `json:"to"`
	SessionID             string     `json:"sessionId,omitempty"`
	FirstResponseTargetMs int64      `json:"firstResponseTargetMs"`
	ResolutionTargetMs    int64      `json:"resolutionTargetMs"`
	Overall               slaMetrics `json:"overall"`
	ByQueue               []slaGroup `json:"byQueue"`
	BySession             []slaGroup `json:"bySession"`
	ByAgent               []slaGroup `json:"byAgent"`
	Alerts                []slaAlert `json:"alerts"`
}

// slaChat accumulates the timeline of a single conversation in the window.
type slaChat struct {
	sessionID    string
	chatJID      string
	name         string
	queueID      string
	firstInbound int64
	firstReply   int64
	closedAt     int64
	agentID      string
	agentLabel   string
	ratings      []int
}

type slaAccumulator struct {
	firstResponses []int64
	resolutions    []int64
	ratingSum      int
	m              slaMetrics
}

func (a *slaAccumulator) add(c *slaChat, respTarget, resTarget int64) {
	a.m.Conversations++
	if c.firstReply > 0 && c.firstInbound > 0 {
		d := c.firstReply - c.firstInbound
		a.m.Answered++
		a.firstResponses = append(a.firstResponses, d)
		if d > a.m.MaxFirstResponseMs {
			a.m.MaxFirstResponseMs = d
		}
		if d > respTarget {
			a.m.FirstResponseBreach++
		}
	} else {
		a.m.Pending++
	}
	if c.closedAt > 0 && c.firstInbound > 0 && c.closedAt >= c.firstInbound {
		d := c.closedAt - c.firstInbound
		a.m.Resolved++
		a.resolutions = append(a.resolutions, d)
		if d > a.m.MaxResolutionMs {
			a.m.MaxResolutionMs = d
		}
		if d > resTarget {
			a.m.ResolutionBreach++
		}
	}
	for _, sc := range c.ratings {
		a.m.RatingCount++
		a.ratingSum += sc
	}
}

func (a *slaAccumulator) finish() slaMetrics {
	a.m.AvgFirstResponseMs = avgMs(a.firstResponses)
	a.m.AvgResolutionMs = avgMs(a.resolutions)
	if a.m.Answered > 0 {
		ok := a.m.Answered - a.m.FirstResponseBreach
		a.m.Compliance = int(float64(ok) / float64(a.m.Answered) * 100)
	}
	if a.m.RatingCount > 0 {
		a.m.RatingAvg = float64(a.ratingSum) / float64(a.m.RatingCount)
	}
	return a.m
}

func avgMs(v []int64) int64 {
	if len(v) == 0 {
		return 0
	}
	var sum int64
	for _, x := range v {
		sum += x
	}
	return sum / int64(len(v))
}

func (s *server) registerSLARoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/reports/sla", s.requireAuth(s.handleSLAReport))
}

func (s *server) handleSLAReport(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	q := r.URL.Query()
	now := time.Now().UnixMilli()
	from, _ := strconv.ParseInt(q.Get("from"), 10, 64)
	to, _ := strconv.ParseInt(q.Get("to"), 10, 64)
	if to == 0 {
		to = now
	}
	if from == 0 {
		from = to - int64(30*24*time.Hour/time.Millisecond)
	}
	respTarget, _ := strconv.ParseInt(q.Get("firstResponseTargetMs"), 10, 64)
	if respTarget <= 0 {
		respTarget = defaultFirstResponseTargetMs
	}
	resTarget, _ := strconv.ParseInt(q.Get("resolutionTargetMs"), 10, 64)
	if resTarget <= 0 {
		resTarget = defaultResolutionTargetMs
	}
	requested := strings.TrimSpace(q.Get("sessionId"))

	visible := s.sessions.infosFor(u.ID, u.IsSuperAdmin())
	sessionNames := map[string]string{}
	sessionIDs := make([]string, 0, len(visible))
	visibleSet := map[string]bool{}
	for _, si := range visible {
		visibleSet[si.ID] = true
		sessionNames[si.ID] = si.Name
		if requested == "" || si.ID == requested {
			sessionIDs = append(sessionIDs, si.ID)
		}
	}
	if requested != "" && !visibleSet[requested] {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such session"})
		return
	}

	queueNames := map[string]string{}
	if s.queues != nil {
		if rows, err := s.queues.List(r.Context(), u.ID, u.TenantID(), u.IsAdmin(), u.IsSuperAdmin()); err == nil {
			for _, qr := range rows {
				queueNames[qr.ID] = qr.Name
			}
		}
	}

	out := slaReport{
		From: from, To: to, SessionID: requested,
		FirstResponseTargetMs: respTarget,
		ResolutionTargetMs:    resTarget,
		ByQueue:               []slaGroup{},
		BySession:             []slaGroup{},
		ByAgent:               []slaGroup{},
		Alerts:                []slaAlert{},
	}

	overall := &slaAccumulator{}
	perQueue := map[string]*slaAccumulator{}
	perSession := map[string]*slaAccumulator{}
	perAgent := map[string]*slaAccumulator{}
	agentLabels := map[string]string{}

	for _, sid := range sessionIDs {
		chats := map[string]*slaChat{}
		if s.messages != nil {
			rows, err := s.messages.listForReport(r.Context(), sid, from, to)
			if err == nil {
				sort.Slice(rows, func(i, j int) bool { return rows[i].Ts < rows[j].Ts })
				for _, m := range rows {
					if isGroupChatJID(m.ChatJID) {
						continue
					}
					c := chats[m.ChatJID]
					if c == nil {
						c = &slaChat{sessionID: sid, chatJID: m.ChatJID}
						chats[m.ChatJID] = c
					}
					if m.FromMe {
						if c.firstInbound > 0 && c.firstReply == 0 {
							c.firstReply = m.Ts
						}
						continue
					}
					if c.firstInbound == 0 {
						c.firstInbound = m.Ts
					}
				}
			}
		}
		if s.chatMeta != nil {
			if metas, err := s.chatMeta.ListBySession(r.Context(), sid); err == nil {
				for jid, meta := range metas {
					if c := chats[jid]; c != nil {
						c.queueID = meta.QueueID
						c.name = meta.Name
						if meta.AssignedUserID != "" {
							c.agentID = meta.AssignedUserID
							c.agentLabel = meta.AssignedUserID
						}
					}
				}
			}
			if closures, err := s.chatMeta.listClosuresInRange(r.Context(), sid, from, to); err == nil {
				for _, cl := range closures {
					if c := chats[cl.ChatJID]; c != nil && cl.ClosedAt > c.closedAt {
						c.closedAt = cl.ClosedAt
						if cl.UserID != "" || cl.UserEmail != "" {
							c.agentID = cl.UserID
							if c.agentID == "" {
								c.agentID = cl.UserEmail
							}
							c.agentLabel = cl.UserEmail
							if c.agentLabel == "" {
								c.agentLabel = cl.UserID
							}
						}
					}
				}
			}
			if ratings, err := s.chatMeta.ListRatingsInRange(r.Context(), sid, from, to); err == nil {
				for _, rt := range ratings {
					if c := chats[rt.ChatJID]; c != nil && rt.Score > 0 {
						c.ratings = append(c.ratings, rt.Score)
					}
				}
			}
		}

		for _, c := range chats {
			if c.firstInbound == 0 {
				continue
			}
			overall.add(c, respTarget, resTarget)
			qk := c.queueID
			if qk == "" {
				qk = "__none__"
			}
			if perQueue[qk] == nil {
				perQueue[qk] = &slaAccumulator{}
			}
			perQueue[qk].add(c, respTarget, resTarget)
			if perSession[sid] == nil {
				perSession[sid] = &slaAccumulator{}
			}
			perSession[sid].add(c, respTarget, resTarget)
			ak := c.agentID
			if ak == "" {
				ak = "__none__"
			}
			if perAgent[ak] == nil {
				perAgent[ak] = &slaAccumulator{}
			}
			if c.agentLabel != "" {
				agentLabels[ak] = c.agentLabel
			}
			perAgent[ak].add(c, respTarget, resTarget)

			alert := slaAlert{
				SessionID: sid, SessionName: sessionNames[sid],
				ChatJID: c.chatJID, Name: c.name,
				QueueID: c.queueID, QueueName: queueNames[c.queueID],
				Ts: c.firstInbound,
			}
			switch {
			case c.firstReply == 0:
				alert.Kind = "unanswered"
				alert.WaitingMs = now - c.firstInbound
				if alert.WaitingMs > respTarget {
					out.Alerts = append(out.Alerts, alert)
				}
			case c.firstReply-c.firstInbound > respTarget:
				alert.Kind = "first-response"
				alert.FirstResponseMs = c.firstReply - c.firstInbound
				if c.closedAt > 0 {
					alert.ResolutionMs = c.closedAt - c.firstInbound
				}
				out.Alerts = append(out.Alerts, alert)
			case c.closedAt > 0 && c.closedAt-c.firstInbound > resTarget:
				alert.Kind = "resolution"
				alert.FirstResponseMs = c.firstReply - c.firstInbound
				alert.ResolutionMs = c.closedAt - c.firstInbound
				out.Alerts = append(out.Alerts, alert)
			}
		}
	}

	out.Overall = overall.finish()
	for id, acc := range perQueue {
		label := queueNames[id]
		if id == "__none__" || label == "" {
			label = "Sem fila"
		}
		out.ByQueue = append(out.ByQueue, slaGroup{ID: id, Label: label, Metrics: acc.finish()})
	}
	sort.Slice(out.ByQueue, func(i, j int) bool {
		return out.ByQueue[i].Metrics.Conversations > out.ByQueue[j].Metrics.Conversations
	})
	for id, acc := range perSession {
		label := sessionNames[id]
		if label == "" {
			label = id
		}
		out.BySession = append(out.BySession, slaGroup{ID: id, Label: label, Metrics: acc.finish()})
	}
	sort.Slice(out.BySession, func(i, j int) bool {
		return out.BySession[i].Metrics.Conversations > out.BySession[j].Metrics.Conversations
	})
	for id, acc := range perAgent {
		label := agentLabels[id]
		if id == "__none__" || label == "" {
			label = "Sem atendente"
		}
		out.ByAgent = append(out.ByAgent, slaGroup{ID: id, Label: label, Metrics: acc.finish()})
	}
	sort.Slice(out.ByAgent, func(i, j int) bool {
		return out.ByAgent[i].Metrics.Conversations > out.ByAgent[j].Metrics.Conversations
	})
	sort.Slice(out.Alerts, func(i, j int) bool {
		a, b := out.Alerts[i], out.Alerts[j]
		wa := a.WaitingMs + a.FirstResponseMs + a.ResolutionMs
		wb := b.WaitingMs + b.FirstResponseMs + b.ResolutionMs
		return wa > wb
	})
	if len(out.Alerts) > 50 {
		out.Alerts = out.Alerts[:50]
	}

	writeJSON(w, http.StatusOK, out)
}
