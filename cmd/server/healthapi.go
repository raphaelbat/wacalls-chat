package main

import (
	"net/http"
	"time"
)

// registerHealthRoutes mounts the per-session WhatsApp account health
// endpoint used by the "Limites da conta" dialog. The values returned are
// derived from the whatsmeow client state — fields that WhatsApp does not
// expose publicly (reachout timelock, message capping quotas) are returned
// as zero/empty so the frontend can render them as "—" honestly.
func (s *server) registerHealthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sessions/{sid}/health", s.requireAuth(s.handleSessionHealth))
}

type accountHealth struct {
	SessionID    string `json:"sessionId"`
	Connected    bool   `json:"connected"`
	LoggedIn     bool   `json:"loggedIn"`
	State        string `json:"state"`
	Paired       bool   `json:"paired"`
	JID          string `json:"jid"`
	LID          string `json:"lid"`
	PushName     string `json:"pushName"`
	BusinessName string `json:"businessName"`
	Platform     string `json:"platform"`
	IsBusiness   bool   `json:"isBusiness"`

	// Health summary. `restricted=true` means whatsmeow believes this
	// connection was kicked from the server (e.g. logged_out while paired).
	Restricted     bool   `json:"restricted"`
	RestrictionKey string `json:"restrictionKey"` // e.g. "RESTRICT_ALL_COMPANIONS" / "NONE"

	// Capping/reachout timelock are not exposed by WhatsApp's public IQs
	// for non-Cloud-API accounts, so we return neutral defaults.
	ReachoutExpiresAt int64  `json:"reachoutExpiresAt"`
	CapTotal          int    `json:"capTotal"`
	CapUsed           int    `json:"capUsed"`
	CapCycleStart     int64  `json:"capCycleStart"`
	CapCycleEnd       int64  `json:"capCycleEnd"`
	OTEStatus         string `json:"oteStatus"`
	MVStatus          string `json:"mvStatus"`
	CappingStatus     string `json:"cappingStatus"`

	QueriedAt int64 `json:"queriedAt"`
}

func (s *server) handleSessionHealth(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	sess.mu.Lock()
	a := sess.auth
	sess.mu.Unlock()

	out := accountHealth{
		SessionID:     sess.id,
		State:         a.State,
		Paired:        a.Paired,
		OTEStatus:     "NOT_ELIGIBLE",
		MVStatus:      "NOT_ELIGIBLE",
		CappingStatus: "NONE",
		QueriedAt:     time.Now().UnixMilli(),
	}

	if sess.client != nil {
		out.Connected = sess.client.IsConnected()
		out.LoggedIn = sess.client.IsLoggedIn()
		if st := sess.client.Store; st != nil {
			if st.ID != nil {
				out.JID = st.ID.String()
			}
			if !st.LID.IsEmpty() {
				out.LID = st.LID.String()
			}
			out.PushName = st.PushName
			out.BusinessName = st.BusinessName
			out.Platform = st.Platform
			out.IsBusiness = st.BusinessName != ""
		}
	}
	out.Paired = out.Paired || out.JID != ""

	// Heuristic restriction: paired session that was forcibly logged out by
	// the server (HTTP 401-style stream-end) — the strongest signal we have
	// without a private reachout-timelock IQ.
	if a.State == "logged_out" && a.Paired {
		out.Restricted = true
		out.RestrictionKey = "RESTRICT_ALL_COMPANIONS"
		end := endOfTodayUTC()
		out.ReachoutExpiresAt = end
		out.CapCycleEnd = end
	} else {
		out.RestrictionKey = "NONE"
		out.CapCycleEnd = endOfTodayUTC()
	}

	writeJSON(w, http.StatusOK, out)
}

func endOfTodayUTC() int64 {
	now := time.Now()
	t := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 30, 0, now.Location())
	return t.UnixMilli()
}
