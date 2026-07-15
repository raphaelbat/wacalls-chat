package main

import (
	"encoding/json"
	"net/http"
)

func (s *server) registerQueueRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/queues", s.requireAuth(s.handleQueueList))
	mux.HandleFunc("POST /api/queues", s.requireAdmin(s.handleQueueCreate))
	mux.HandleFunc("PUT /api/queues/{id}", s.requireAdmin(s.handleQueueUpdate))
	mux.HandleFunc("DELETE /api/queues/{id}", s.requireAdmin(s.handleQueueDelete))
}

func (s *server) handleQueueList(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	rows, err := s.queues.List(r.Context(), u.ID, u.TenantID(), u.IsAdmin(), u.IsSuperAdmin())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"queues": rows})
}

func (s *server) handleQueueCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	u := currentUserFromReq(r)
	if list, err := s.queues.List(r.Context(), u.ID, u.TenantID(), true, false); err == nil {
		if qerr := s.enforceQuota(r.Context(), "filas", len(list)); qerr != nil {
			writeQuotaError(w, qerr)
			return
		}
	}
	// Persist queues against the tenant root so every admin / operator in
	// the same company sees them, regardless of which sub-user created it.
	row, err := s.queues.Create(r.Context(), body.Name, body.Color, u.TenantID())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (s *server) handleQueueUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.canManageQueue(r, id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such queue"})
		return
	}
	raw := map[string]json.RawMessage{}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	existing, err := s.queues.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such queue"})
		return
	}
	name := existing.Name
	color := existing.Color
	extras := queueExtras{
		OrderBot:         existing.OrderBot,
		CloseTicket:      existing.CloseTicket,
		Rotation:         existing.Rotation,
		RotationInterval: existing.RotationInterval,
		RotationMode:     existing.RotationMode,
		AutoRandomize:    existing.AutoRandomize,
		AgentID:          existing.AgentID,
		Greeting:         existing.Greeting,
	}
	_ = mergeStr(raw, "name", &name)
	_ = mergeStr(raw, "color", &color)
	_ = mergeStr(raw, "orderBot", &extras.OrderBot)
	_ = mergeBool(raw, "closeTicket", &extras.CloseTicket)
	_ = mergeBool(raw, "rotation", &extras.Rotation)
	_ = mergeStr(raw, "rotationInterval", &extras.RotationInterval)
	_ = mergeStr(raw, "rotationMode", &extras.RotationMode)
	_ = mergeBool(raw, "autoRandomize", &extras.AutoRandomize)
	_ = mergeStr(raw, "agentId", &extras.AgentID)
	_ = mergeStr(raw, "greeting", &extras.Greeting)
	if err := s.queues.UpdateFull(r.Context(), id, name, color, extras); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func mergeStr(raw map[string]json.RawMessage, key string, dst *string) error {
	if v, ok := raw[key]; ok {
		return json.Unmarshal(v, dst)
	}
	return nil
}

func mergeBool(raw map[string]json.RawMessage, key string, dst *bool) error {
	if v, ok := raw[key]; ok {
		return json.Unmarshal(v, dst)
	}
	return nil
}

func (s *server) handleQueueDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.canManageQueue(r, id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such queue"})
		return
	}
	if err := s.queues.Delete(r.Context(), id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) canManageQueue(r *http.Request, id string) bool {
	u := currentUserFromReq(r)
	if u == nil {
		return false
	}
	if u.IsSuperAdmin() {
		return true
	}
	row, err := s.queues.Get(r.Context(), id)
	if err != nil {
		return false
	}
	// Allow when the queue belongs to the caller's tenant. owner_id is
	// either the tenant root itself or any sub-user inside that tenant.
	if row.OwnerID == u.TenantID() {
		return true
	}
	if row.OwnerID == u.ID {
		return true
	}
	pid, err := s.auth.ParentOf(r.Context(), row.OwnerID)
	if err == nil && pid != "" && pid == u.TenantID() {
		return true
	}
	return false
}
