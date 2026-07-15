package main

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type reportCounter struct {
	Total    int `json:"total"`
	Inbound  int `json:"inbound"`
	Outbound int `json:"outbound"`
}

type reportCalls struct {
	Total           int   `json:"total"`
	Inbound         int   `json:"inbound"`
	Outbound        int   `json:"outbound"`
	Answered        int   `json:"answered"`
	Missed          int   `json:"missed"`
	Video           int   `json:"video"`
	TotalDurationMs int64 `json:"totalDurationMs"`
	AvgDurationMs   int64 `json:"avgDurationMs"`
}

type reportTickets struct {
	Closed  int `json:"closed"`
	Waiting int `json:"waiting"`
	Open    int `json:"open"`
}

type reportDaily struct {
	Day           string `json:"day"`
	MessagesIn    int    `json:"messagesIn"`
	MessagesOut   int    `json:"messagesOut"`
	CallsIn       int    `json:"callsIn"`
	CallsOut      int    `json:"callsOut"`
	CallsAnswered int    `json:"callsAnswered"`
	CallsMissed   int    `json:"callsMissed"`
	TicketsClosed int    `json:"ticketsClosed"`
}

type reportLabelCount struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type reportAgentCount struct {
	UserID string `json:"userId"`
	Email  string `json:"email,omitempty"`
	Closed int    `json:"closed"`
}

type reportRatings struct {
	Total   int `json:"total"`
	Good    int `json:"good"`
	Bad     int `json:"bad"`
	Awful   int `json:"awful"`
	Average int `json:"average"`
}

type reportSummary struct {
	From           int64              `json:"from"`
	To             int64              `json:"to"`
	SessionID      string             `json:"sessionId,omitempty"`
	Messages       reportCounter      `json:"messages"`
	Calls          reportCalls        `json:"calls"`
	Tickets        reportTickets      `json:"tickets"`
	Daily          []reportDaily      `json:"daily"`
	ClosureReasons []reportLabelCount `json:"closureReasons"`
	Agents         []reportAgentCount `json:"agents"`
	Ratings        reportRatings      `json:"ratings"`
}

func (s *server) registerReportRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/reports/summary", s.requireAuth(s.handleReportSummary))
}

func (s *server) handleReportSummary(w http.ResponseWriter, r *http.Request) {
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
	requested := strings.TrimSpace(q.Get("sessionId"))

	visible := s.sessions.infosFor(u.ID, u.IsSuperAdmin())
	sessionIDs := make([]string, 0, len(visible))
	visibleSet := map[string]bool{}
	for _, si := range visible {
		visibleSet[si.ID] = true
		if requested == "" || si.ID == requested {
			sessionIDs = append(sessionIDs, si.ID)
		}
	}
	if requested != "" && !visibleSet[requested] {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such session"})
		return
	}

	summary := reportSummary{From: from, To: to, SessionID: requested}
	daily := makeDailyBuckets(from, to)
	reasonCounts := map[string]int{}
	agents := map[string]*reportAgentCount{}

	for _, sid := range sessionIDs {
		if s.messages != nil {
			rows, err := s.messages.listForReport(r.Context(), sid, from, to)
			if err == nil {
				for _, m := range rows {
					if isGroupChatJID(m.ChatJID) {
						continue
					}
					summary.Messages.Total++
					b := daily[reportDayKey(m.Ts)]
					if m.FromMe {
						summary.Messages.Outbound++
						if b != nil {
							b.MessagesOut++
						}
					} else {
						summary.Messages.Inbound++
						if b != nil {
							b.MessagesIn++
						}
					}
				}
			}
		}

		if s.calls != nil {
			calls, err := s.calls.ListBetween(r.Context(), sid, from, to)
			if err == nil {
				for _, c := range calls {
					summary.Calls.Total++
					b := daily[reportDayKey(c.StartedAt)]
					if strings.EqualFold(c.Direction, "outbound") {
						summary.Calls.Outbound++
						if b != nil {
							b.CallsOut++
						}
					} else {
						summary.Calls.Inbound++
						if b != nil {
							b.CallsIn++
						}
					}
					if c.Answered {
						summary.Calls.Answered++
						summary.Calls.TotalDurationMs += c.DurationMs
						if b != nil {
							b.CallsAnswered++
						}
					} else if !strings.EqualFold(c.Direction, "outbound") {
						summary.Calls.Missed++
						if b != nil {
							b.CallsMissed++
						}
					}
					if c.Video {
						summary.Calls.Video++
					}
				}
			}
		}

		if s.chatMeta != nil {
			metas, err := s.chatMeta.ListBySession(r.Context(), sid)
			if err == nil {
				for jid, m := range metas {
					if m.IsGroup || isGroupChatJID(jid) {
						continue
					}
					switch m.Status {
					case ChatStatusClosed:
						summary.Tickets.Closed++
					case ChatStatusOpen:
						summary.Tickets.Open++
					default:
						summary.Tickets.Waiting++
					}
				}
			}

			closures, err := s.chatMeta.listClosuresInRange(r.Context(), sid, from, to)
			if err == nil {
				for _, c := range closures {
					label := strings.TrimSpace(c.Reason)
					if label == "" {
						label = "Sem descrição"
					}
					reasonCounts[label]++
					key := c.UserID
					if key == "" {
						key = c.UserEmail
					}
					if key == "" {
						key = "Sistema"
					}
					a := agents[key]
					if a == nil {
						a = &reportAgentCount{UserID: c.UserID, Email: c.UserEmail}
						if a.UserID == "" {
							a.UserID = key
						}
						agents[key] = a
					}
					a.Closed++
					if b := daily[reportDayKey(c.ClosedAt)]; b != nil {
						b.TicketsClosed++
					}
				}
			}
		}
	}

	if summary.Calls.Answered > 0 {
		summary.Calls.AvgDurationMs = summary.Calls.TotalDurationMs / int64(summary.Calls.Answered)
	}
	summary.Daily = flattenDaily(daily)
	for label, count := range reasonCounts {
		summary.ClosureReasons = append(summary.ClosureReasons, reportLabelCount{Label: label, Count: count})
	}
	sort.Slice(summary.ClosureReasons, func(i, j int) bool { return summary.ClosureReasons[i].Count > summary.ClosureReasons[j].Count })
	for _, a := range agents {
		summary.Agents = append(summary.Agents, *a)
	}
	sort.Slice(summary.Agents, func(i, j int) bool { return summary.Agents[i].Closed > summary.Agents[j].Closed })

	writeJSON(w, http.StatusOK, summary)
}

func reportDayKey(ts int64) string {
	return time.UnixMilli(ts).Format("2006-01-02")
}

func makeDailyBuckets(from, to int64) map[string]*reportDaily {
	out := map[string]*reportDaily{}
	start := time.UnixMilli(from)
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	end := time.UnixMilli(to)
	end = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, end.Location())
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		out[key] = &reportDaily{Day: key}
	}
	return out
}

func flattenDaily(m map[string]*reportDaily) []reportDaily {
	out := make([]reportDaily, 0, len(m))
	for _, d := range m {
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Day < out[j].Day })
	return out
}
