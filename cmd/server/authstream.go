package main

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// authStreamHub keeps track of open SSE connections keyed by session token so
// we can push a "revoked" event the moment the same user logs in from another
// browser. Each token owns at most one channel.
type authStreamHub struct {
	mu   sync.Mutex
	subs map[string]chan struct{} // token -> close signal
}

func newAuthStreamHub() *authStreamHub {
	return &authStreamHub{subs: map[string]chan struct{}{}}
}

func (h *authStreamHub) add(token string) chan struct{} {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	if prev, ok := h.subs[token]; ok {
		// Same token reconnecting: close the previous channel.
		select {
		case prev <- struct{}{}:
		default:
		}
	}
	h.subs[token] = ch
	h.mu.Unlock()
	return ch
}

func (h *authStreamHub) remove(token string, ch chan struct{}) {
	h.mu.Lock()
	if cur, ok := h.subs[token]; ok && cur == ch {
		delete(h.subs, token)
	}
	h.mu.Unlock()
}

// Revoke pushes a "revoked" signal to the matching token (if connected).
func (h *authStreamHub) Revoke(token string) {
	h.mu.Lock()
	ch, ok := h.subs[token]
	if ok {
		delete(h.subs, token)
	}
	h.mu.Unlock()
	if ok {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// handleAuthStream is a tiny SSE endpoint that emits a single "revoked" event
// when the current session token gets invalidated (typically because the same
// user logged in from another browser).
func (s *server) handleAuthStream(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(authCookieName)
	if err != nil || c.Value == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	token := c.Value
	// Confirm the token is currently valid.
	if _, err := s.auth.UserByToken(r.Context(), token); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	revoke := s.authStream.add(token)
	defer s.authStream.remove(token, revoke)

	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-revoke:
			fmt.Fprint(w, "event: revoked\ndata: {}\n\n")
			flusher.Flush()
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
