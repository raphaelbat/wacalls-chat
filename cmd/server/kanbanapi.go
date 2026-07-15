package main

import (
	"encoding/json"
	"net/http"
)

func (s *server) registerKanbanRoutes(mux *http.ServeMux) {
	// Boards
	mux.HandleFunc("GET /api/kanban/boards", s.requireAuth(s.handleKBBoardList))
	mux.HandleFunc("POST /api/kanban/boards", s.requireAuth(s.handleKBBoardCreate))
	mux.HandleFunc("GET /api/kanban/boards/{id}", s.requireAuth(s.handleKBBoardGet))
	mux.HandleFunc("PUT /api/kanban/boards/{id}", s.requireAuth(s.handleKBBoardUpdate))
	mux.HandleFunc("DELETE /api/kanban/boards/{id}", s.requireAuth(s.handleKBBoardDelete))

	// Columns (nested under board for creation; per-id for mutations).
	mux.HandleFunc("POST /api/kanban/boards/{id}/columns", s.requireAuth(s.handleKBColumnCreate))
	mux.HandleFunc("PUT /api/kanban/columns/{id}", s.requireAuth(s.handleKBColumnUpdate))
	mux.HandleFunc("DELETE /api/kanban/columns/{id}", s.requireAuth(s.handleKBColumnDelete))

	// Cards
	mux.HandleFunc("POST /api/kanban/boards/{id}/cards", s.requireAuth(s.handleKBCardCreate))
	mux.HandleFunc("PUT /api/kanban/cards/{id}", s.requireAuth(s.handleKBCardUpdate))
	mux.HandleFunc("POST /api/kanban/cards/{id}/move", s.requireAuth(s.handleKBCardMove))
	mux.HandleFunc("DELETE /api/kanban/cards/{id}", s.requireAuth(s.handleKBCardDelete))

	// Automations
	mux.HandleFunc("GET /api/kanban/boards/{id}/automations", s.requireAuth(s.handleKBAutomList))
	mux.HandleFunc("POST /api/kanban/boards/{id}/automations", s.requireAuth(s.handleKBAutomUpsert))
	mux.HandleFunc("DELETE /api/kanban/automations/{id}", s.requireAuth(s.handleKBAutomDelete))

	// Lookup: which cards reference the current chat. Used by ChatView to
	// surface the linked kanban entries inline.
	mux.HandleFunc("GET /api/kanban/cards/by-chat", s.requireAuth(s.handleKBCardsByChat))
}

// canManageBoard centralises the ownership check used by every mutating
// endpoint. Admins bypass the check entirely.
func (s *server) canManageBoard(r *http.Request, boardID string) bool {
	u := currentUserFromReq(r)
	if u == nil {
		return false
	}
	if u.IsAdmin() {
		return true
	}
	b, err := s.kanban.GetBoard(r.Context(), boardID)
	if err != nil {
		return false
	}
	return b.OwnerID == "" || b.OwnerID == u.ID
}

// ----- boards -----

func (s *server) handleKBBoardList(w http.ResponseWriter, r *http.Request) {
	u := currentUserFromReq(r)
	rows, err := s.kanban.ListBoards(r.Context(), u.ID, u.IsAdmin())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"boards": rows})
}

func (s *server) handleKBBoardCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name, Color, Description string
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	u := currentUserFromReq(r)
	b, err := s.kanban.CreateBoard(r.Context(), body.Name, body.Color, body.Description, u.ID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *server) handleKBBoardGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.canManageBoard(r, id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	board, err := s.kanban.GetBoard(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	cols, err := s.kanban.ListColumns(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Materialize a card for every active chat (atendimento) the user can
	// see that doesn't yet have one on this board, so the board reflects
	// the conversation queue out-of-the-box instead of staying empty.
	if u := currentUserFromReq(r); u != nil {
		s.backfillKanbanCards(r.Context(), id, cols, u.ID, u.IsSuperAdmin())
	}
	cards, err := s.kanban.ListCards(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	autom, _ := s.kanban.ListAutomations(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{"board": board, "columns": cols, "cards": cards, "automations": autom})
}

func (s *server) handleKBBoardUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.canManageBoard(r, id) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	var body struct{ Name, Color, Description string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if err := s.kanban.UpdateBoard(r.Context(), id, body.Name, body.Color, body.Description); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleKBBoardDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.canManageBoard(r, id) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if err := s.kanban.DeleteBoard(r.Context(), id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ----- columns -----

func (s *server) handleKBColumnCreate(w http.ResponseWriter, r *http.Request) {
	boardID := r.PathValue("id")
	if !s.canManageBoard(r, boardID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	var body struct{ Name, Color, StageType string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	c, err := s.kanban.CreateColumn(r.Context(), boardID, body.Name, body.Color, body.StageType)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *server) handleKBColumnUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	col, err := s.kanban.GetColumn(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if !s.canManageBoard(r, col.BoardID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	var body struct{ Name, Color, StageType string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if body.StageType == "" {
		body.StageType = col.StageType
	}
	if err := s.kanban.UpdateColumn(r.Context(), id, body.Name, body.Color, body.StageType); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleKBColumnDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	col, err := s.kanban.GetColumn(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if !s.canManageBoard(r, col.BoardID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if err := s.kanban.DeleteColumn(r.Context(), id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ----- cards -----

func (s *server) handleKBCardCreate(w http.ResponseWriter, r *http.Request) {
	boardID := r.PathValue("id")
	if !s.canManageBoard(r, boardID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	var body struct {
		ColumnID, Title, Description, Color string
		SessionID, ChatJID, AssigneeID      string
		DueAt                               int64
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	c, err := s.kanban.CreateCard(r.Context(), cardCreate{
		BoardID: boardID, ColumnID: body.ColumnID, Title: body.Title,
		Description: body.Description, Color: body.Color,
		SessionID: body.SessionID, ChatJID: body.ChatJID,
		AssigneeID: body.AssigneeID, DueAt: body.DueAt,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *server) handleKBCardUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	card, err := s.kanban.GetCard(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if !s.canManageBoard(r, card.BoardID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	var body struct {
		Title, Description, Color      *string
		AssigneeID, ChatJID, SessionID *string
		DueAt                          *int64
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if err := s.kanban.UpdateCard(r.Context(), id, cardUpdate{
		Title: body.Title, Description: body.Description, Color: body.Color,
		AssigneeID: body.AssigneeID, ChatJID: body.ChatJID, SessionID: body.SessionID,
		DueAt: body.DueAt,
	}); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleKBCardMove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	card, err := s.kanban.GetCard(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if !s.canManageBoard(r, card.BoardID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	var body struct {
		ColumnID string `json:"columnId"`
		Position int    `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if err := s.kanban.MoveCard(r.Context(), id, body.ColumnID, body.Position); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleKBCardDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	card, err := s.kanban.GetCard(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if !s.canManageBoard(r, card.BoardID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	if err := s.kanban.DeleteCard(r.Context(), id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleKBCardsByChat(w http.ResponseWriter, r *http.Request) {
	sid := r.URL.Query().Get("sid")
	jid := r.URL.Query().Get("jid")
	if sid == "" || jid == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sid and jid required"})
		return
	}
	cards, err := s.kanban.CardsByChat(r.Context(), sid, jid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": cards})
}

// ----- automations -----

func (s *server) handleKBAutomList(w http.ResponseWriter, r *http.Request) {
	boardID := r.PathValue("id")
	if !s.canManageBoard(r, boardID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	rows, err := s.kanban.ListAutomations(r.Context(), boardID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"automations": rows})
}

func (s *server) handleKBAutomUpsert(w http.ResponseWriter, r *http.Request) {
	boardID := r.PathValue("id")
	if !s.canManageBoard(r, boardID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	var body kanbanAutomation
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	body.BoardID = boardID
	out, err := s.kanban.UpsertAutomation(r.Context(), body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) handleKBAutomDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.kanban.DeleteAutomation(r.Context(), id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
