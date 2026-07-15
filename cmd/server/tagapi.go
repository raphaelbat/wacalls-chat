package main

import (
	"encoding/json"
	"net/http"
)

func (s *server) registerTagRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tags", s.requireAuth(s.handleTagList))
	mux.HandleFunc("POST /api/tags", s.requireAuth(s.handleTagCreate))
	mux.HandleFunc("PUT /api/tags/{id}", s.requireAuth(s.handleTagUpdate))
	mux.HandleFunc("DELETE /api/tags/{id}", s.requireAuth(s.handleTagDelete))
	mux.HandleFunc("GET /api/sessions/{sid}/chats/{jid}/tags", s.requireAuth(s.handleChatTagsList))
	mux.HandleFunc("POST /api/sessions/{sid}/chats/{jid}/tags", s.requireAuth(s.handleChatTagsAttach))
	mux.HandleFunc("DELETE /api/sessions/{sid}/chats/{jid}/tags/{tagId}", s.requireAuth(s.handleChatTagsDetach))
}

func (s *server) handleTagList(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	rows, err := s.tags.List(r.Context(), u.ID, u.IsAdmin())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": rows})
}

func (s *server) handleTagCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	u := currentUserFromReq(r)
	row, err := s.tags.Create(r.Context(), body.Name, body.Color, u.ID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (s *server) handleTagUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.canManageTag(r, id) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	var body struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if err := s.tags.Update(r.Context(), id, body.Name, body.Color); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleTagDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.canManageTag(r, id) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if err := s.tags.Delete(r.Context(), id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// canManageTag enforces ownership: admins may edit any tag, operators only
// tags they created themselves.
func (s *server) canManageTag(r *http.Request, id string) bool {
	u := currentUserFromReq(r)
	if u == nil {
		return false
	}
	if u.IsAdmin() {
		return true
	}
	row, err := s.tags.Get(r.Context(), id)
	if err != nil {
		return false
	}
	return row.OwnerID == u.ID
}

func (s *server) handleChatTagsList(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	rows, err := s.tags.ListForChat(r.Context(), sess.id, r.PathValue("jid"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": rows})
}

func (s *server) handleChatTagsAttach(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	var body struct {
		TagID string `json:"tagId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	u := currentUserFromReq(r)
	ownerID := ""
	if u != nil {
		ownerID = u.ID
	}
	if err := s.tags.AttachToChat(r.Context(), sess.id, r.PathValue("jid"), body.TagID, ownerID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	rows, _ := s.tags.ListForChat(r.Context(), sess.id, r.PathValue("jid"))
	writeJSON(w, http.StatusOK, map[string]any{"tags": rows})
}

func (s *server) handleChatTagsDetach(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r, r.PathValue("sid"))
	if sess == nil {
		return
	}
	if err := s.tags.DetachFromChat(r.Context(), sess.id, r.PathValue("jid"), r.PathValue("tagId")); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
