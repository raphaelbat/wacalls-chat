package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// kanbanBoard / kanbanColumn / kanbanCard model a lightweight Trello-style
// pipeline. Boards are owned by users (admins see every board). Columns are
// ordered by `position`, cards likewise. A card can optionally be linked to a
// WhatsApp chat through `session_id` + `chat_jid` so that operators can pull
// the underlying conversation back from the kanban surface.
type kanbanBoard struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
	OwnerID     string `json:"ownerId,omitempty"`
	CreatedAt   int64  `json:"createdAt"`
}

type kanbanColumn struct {
	ID       string `json:"id"`
	BoardID  string `json:"boardId"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	Position int    `json:"position"`
	// StageType marks the funnel meaning of this stage: "open" (default),
	// "won" (deal closed) or "lost" (deal dropped). Used by the Pipeline
	// UI to render colored badges and to compute funnel KPIs.
	StageType string `json:"stageType"`
	CreatedAt int64  `json:"createdAt"`
}

type kanbanCard struct {
	ID          string `json:"id"`
	BoardID     string `json:"boardId"`
	ColumnID    string `json:"columnId"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Color       string `json:"color"`
	Position    int    `json:"position"`
	SessionID   string `json:"sessionId,omitempty"`
	ChatJID     string `json:"chatJid,omitempty"`
	AssigneeID  string `json:"assigneeId,omitempty"`
	DueAt       int64  `json:"dueAt,omitempty"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

// kanbanAutomation is a tiny rule: when an external event happens for a
// chat, move (or create) the linked card to the configured target stage.
// `WhenStageID` is the stage the card must currently be in for the rule to
// fire — empty string means "no stage" (card absent / unlinked), which is
// how new contacts get bootstrapped into the pipeline.
type kanbanAutomation struct {
	ID            string `json:"id"`
	BoardID       string `json:"boardId"`
	Kind          string `json:"kind"` // replied | answered_call | new_contact
	WhenStageID   string `json:"whenStageId"`
	TargetStageID string `json:"targetStageId"`
	Enabled       bool   `json:"enabled"`
	CreatedAt     int64  `json:"createdAt"`
}

type kanbanStore struct{ db *sql.DB }

var ErrKanbanNotFound = errors.New("kanban entity not found")

func newKanbanStore(ctx context.Context, db *sql.DB) (*kanbanStore, error) {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS kanban_boards (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			color TEXT NOT NULL DEFAULT '#6366f1',
			description TEXT NOT NULL DEFAULT '',
			owner_id TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS kanban_columns (
			id TEXT PRIMARY KEY,
			board_id TEXT NOT NULL,
			name TEXT NOT NULL,
			color TEXT NOT NULL DEFAULT '#64748b',
			position INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS kanban_cards (
			id TEXT PRIMARY KEY,
			board_id TEXT NOT NULL,
			column_id TEXT NOT NULL,
			title TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			color TEXT NOT NULL DEFAULT '',
			position INTEGER NOT NULL DEFAULT 0,
			session_id TEXT NOT NULL DEFAULT '',
			chat_jid TEXT NOT NULL DEFAULT '',
			assignee_id TEXT NOT NULL DEFAULT '',
			due_at INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_kanban_cols_board ON kanban_columns(board_id, position)`,
		`CREATE INDEX IF NOT EXISTS idx_kanban_cards_col ON kanban_cards(column_id, position)`,
		`CREATE INDEX IF NOT EXISTS idx_kanban_cards_chat ON kanban_cards(session_id, chat_jid)`,
		`CREATE TABLE IF NOT EXISTS kanban_automations (
			id TEXT PRIMARY KEY,
			board_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			when_stage_id TEXT NOT NULL DEFAULT '',
			target_stage_id TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_kanban_autom_board ON kanban_automations(board_id)`,
	}
	for _, q := range stmts {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return nil, err
		}
	}
	// Best-effort additive migration: add stage_type to existing installs.
	_, _ = db.ExecContext(ctx, `ALTER TABLE kanban_columns ADD COLUMN stage_type TEXT NOT NULL DEFAULT 'open'`)
	return &kanbanStore{db: db}, nil
}

// ------- Boards -------

func (s *kanbanStore) ListBoards(ctx context.Context, ownerID string, isAdmin bool) ([]kanbanBoard, error) {
	var rows *sql.Rows
	var err error
	if isAdmin {
		rows, err = s.db.QueryContext(ctx, `SELECT id,name,color,description,owner_id,created_at FROM kanban_boards ORDER BY created_at ASC`)
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT id,name,color,description,owner_id,created_at FROM kanban_boards WHERE owner_id = ? OR owner_id = '' ORDER BY created_at ASC`, ownerID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []kanbanBoard{}
	for rows.Next() {
		var b kanbanBoard
		if err := rows.Scan(&b.ID, &b.Name, &b.Color, &b.Description, &b.OwnerID, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *kanbanStore) GetBoard(ctx context.Context, id string) (kanbanBoard, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,name,color,description,owner_id,created_at FROM kanban_boards WHERE id = ?`, id)
	var b kanbanBoard
	if err := row.Scan(&b.ID, &b.Name, &b.Color, &b.Description, &b.OwnerID, &b.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return kanbanBoard{}, ErrKanbanNotFound
		}
		return kanbanBoard{}, err
	}
	return b, nil
}

func (s *kanbanStore) CreateBoard(ctx context.Context, name, color, description, ownerID string) (kanbanBoard, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return kanbanBoard{}, errors.New("name required")
	}
	if color == "" {
		color = "#6366f1"
	}
	b := kanbanBoard{ID: newID(), Name: name, Color: color, Description: description, OwnerID: ownerID, CreatedAt: time.Now().Unix()}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO kanban_boards (id,name,color,description,owner_id,created_at) VALUES (?,?,?,?,?,?)`,
		b.ID, b.Name, b.Color, b.Description, b.OwnerID, b.CreatedAt); err != nil {
		return kanbanBoard{}, err
	}
	// Seed the board with three sensible defaults so operators don't stare at
	// an empty board on first open.
	defaults := []string{"A fazer", "Em andamento", "Concluído"}
	colors := []string{"#94a3b8", "#3b82f6", "#10b981"}
	stypes := []string{"open", "open", "won"}
	for i, n := range defaults {
		if _, err := s.CreateColumn(ctx, b.ID, n, colors[i], stypes[i]); err != nil {
			return b, err
		}
	}
	return b, nil
}

func (s *kanbanStore) UpdateBoard(ctx context.Context, id, name, color, description string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE kanban_boards SET name=?, color=?, description=? WHERE id=?`,
		strings.TrimSpace(name), color, description, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrKanbanNotFound
	}
	return nil
}

func (s *kanbanStore) DeleteBoard(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM kanban_cards WHERE board_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM kanban_columns WHERE board_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM kanban_boards WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// ------- Columns -------

func (s *kanbanStore) ListColumns(ctx context.Context, boardID string) ([]kanbanColumn, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,board_id,name,color,position,COALESCE(stage_type,'open'),created_at FROM kanban_columns WHERE board_id = ? ORDER BY position ASC, created_at ASC`,
		boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []kanbanColumn{}
	for rows.Next() {
		var c kanbanColumn
		if err := rows.Scan(&c.ID, &c.BoardID, &c.Name, &c.Color, &c.Position, &c.StageType, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *kanbanStore) CreateColumn(ctx context.Context, boardID, name, color, stageType string) (kanbanColumn, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return kanbanColumn{}, errors.New("name required")
	}
	if color == "" {
		color = "#64748b"
	}
	stageType = normalizeStageType(stageType)
	var pos int
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),-1)+1 FROM kanban_columns WHERE board_id = ?`, boardID).Scan(&pos)
	c := kanbanColumn{ID: newID(), BoardID: boardID, Name: name, Color: color, Position: pos, StageType: stageType, CreatedAt: time.Now().Unix()}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO kanban_columns (id,board_id,name,color,position,stage_type,created_at) VALUES (?,?,?,?,?,?,?)`,
		c.ID, c.BoardID, c.Name, c.Color, c.Position, c.StageType, c.CreatedAt); err != nil {
		return kanbanColumn{}, err
	}
	return c, nil
}

func (s *kanbanStore) UpdateColumn(ctx context.Context, id, name, color, stageType string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE kanban_columns SET name=?, color=?, stage_type=? WHERE id=?`,
		strings.TrimSpace(name), color, normalizeStageType(stageType), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrKanbanNotFound
	}
	return nil
}

func (s *kanbanStore) GetColumn(ctx context.Context, id string) (kanbanColumn, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,board_id,name,color,position,COALESCE(stage_type,'open'),created_at FROM kanban_columns WHERE id=?`, id)
	var c kanbanColumn
	if err := row.Scan(&c.ID, &c.BoardID, &c.Name, &c.Color, &c.Position, &c.StageType, &c.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return kanbanColumn{}, ErrKanbanNotFound
		}
		return kanbanColumn{}, err
	}
	return c, nil
}

func normalizeStageType(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "won":
		return "won"
	case "lost":
		return "lost"
	default:
		return "open"
	}
}

func (s *kanbanStore) DeleteColumn(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM kanban_cards WHERE column_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM kanban_columns WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// ------- Cards -------

func (s *kanbanStore) ListCards(ctx context.Context, boardID string) ([]kanbanCard, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,board_id,column_id,title,description,color,position,session_id,chat_jid,assignee_id,due_at,created_at,updated_at
		 FROM kanban_cards WHERE board_id = ? ORDER BY position ASC, created_at ASC`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []kanbanCard{}
	for rows.Next() {
		var c kanbanCard
		if err := rows.Scan(&c.ID, &c.BoardID, &c.ColumnID, &c.Title, &c.Description, &c.Color, &c.Position,
			&c.SessionID, &c.ChatJID, &c.AssigneeID, &c.DueAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *kanbanStore) CardsByChat(ctx context.Context, sessionID, chatJID string) ([]kanbanCard, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,board_id,column_id,title,description,color,position,session_id,chat_jid,assignee_id,due_at,created_at,updated_at
		 FROM kanban_cards WHERE session_id = ? AND chat_jid = ? ORDER BY updated_at DESC`,
		sessionID, chatJID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []kanbanCard{}
	for rows.Next() {
		var c kanbanCard
		if err := rows.Scan(&c.ID, &c.BoardID, &c.ColumnID, &c.Title, &c.Description, &c.Color, &c.Position,
			&c.SessionID, &c.ChatJID, &c.AssigneeID, &c.DueAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

type cardCreate struct {
	BoardID, ColumnID, Title, Description, Color string
	SessionID, ChatJID, AssigneeID               string
	DueAt                                        int64
}

func (s *kanbanStore) CreateCard(ctx context.Context, in cardCreate) (kanbanCard, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return kanbanCard{}, errors.New("title required")
	}
	if in.ColumnID == "" || in.BoardID == "" {
		return kanbanCard{}, errors.New("board and column required")
	}
	var pos int
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),-1)+1 FROM kanban_cards WHERE column_id = ?`, in.ColumnID).Scan(&pos)
	now := time.Now().Unix()
	c := kanbanCard{
		ID: newID(), BoardID: in.BoardID, ColumnID: in.ColumnID,
		Title: in.Title, Description: in.Description, Color: in.Color, Position: pos,
		SessionID: in.SessionID, ChatJID: in.ChatJID, AssigneeID: in.AssigneeID, DueAt: in.DueAt,
		CreatedAt: now, UpdatedAt: now,
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO kanban_cards (id,board_id,column_id,title,description,color,position,session_id,chat_jid,assignee_id,due_at,created_at,updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.BoardID, c.ColumnID, c.Title, c.Description, c.Color, c.Position,
		c.SessionID, c.ChatJID, c.AssigneeID, c.DueAt, c.CreatedAt, c.UpdatedAt); err != nil {
		return kanbanCard{}, err
	}
	return c, nil
}

func (s *kanbanStore) GetCard(ctx context.Context, id string) (kanbanCard, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id,board_id,column_id,title,description,color,position,session_id,chat_jid,assignee_id,due_at,created_at,updated_at
		 FROM kanban_cards WHERE id = ?`, id)
	var c kanbanCard
	if err := row.Scan(&c.ID, &c.BoardID, &c.ColumnID, &c.Title, &c.Description, &c.Color, &c.Position,
		&c.SessionID, &c.ChatJID, &c.AssigneeID, &c.DueAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return kanbanCard{}, ErrKanbanNotFound
		}
		return kanbanCard{}, err
	}
	return c, nil
}

// UpdateCard applies a partial set of fields. Nil values are left untouched.
type cardUpdate struct {
	Title, Description, Color      *string
	AssigneeID, ChatJID, SessionID *string
	DueAt                          *int64
}

func (s *kanbanStore) UpdateCard(ctx context.Context, id string, u cardUpdate) error {
	sets := []string{"updated_at = ?"}
	args := []any{time.Now().Unix()}
	if u.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, strings.TrimSpace(*u.Title))
	}
	if u.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *u.Description)
	}
	if u.Color != nil {
		sets = append(sets, "color = ?")
		args = append(args, *u.Color)
	}
	if u.AssigneeID != nil {
		sets = append(sets, "assignee_id = ?")
		args = append(args, *u.AssigneeID)
	}
	if u.ChatJID != nil {
		sets = append(sets, "chat_jid = ?")
		args = append(args, *u.ChatJID)
	}
	if u.SessionID != nil {
		sets = append(sets, "session_id = ?")
		args = append(args, *u.SessionID)
	}
	if u.DueAt != nil {
		sets = append(sets, "due_at = ?")
		args = append(args, *u.DueAt)
	}
	args = append(args, id)
	q := "UPDATE kanban_cards SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrKanbanNotFound
	}
	return nil
}

// MoveCard relocates a card to (columnID, position). Other cards in the target
// column are shifted so positions remain contiguous and unique-ish for ordering.
func (s *kanbanStore) MoveCard(ctx context.Context, id, columnID string, position int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE kanban_cards SET position = position + 1 WHERE column_id = ? AND position >= ?`,
		columnID, position); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE kanban_cards SET column_id = ?, position = ?, updated_at = ? WHERE id = ?`,
		columnID, position, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrKanbanNotFound
	}
	return tx.Commit()
}

func (s *kanbanStore) DeleteCard(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM kanban_cards WHERE id = ?`, id)
	return err
}

// ------- Automations -------

func (s *kanbanStore) ListAutomations(ctx context.Context, boardID string) ([]kanbanAutomation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,board_id,kind,when_stage_id,target_stage_id,enabled,created_at
		 FROM kanban_automations WHERE board_id = ? ORDER BY created_at ASC`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []kanbanAutomation{}
	for rows.Next() {
		var a kanbanAutomation
		var en int
		if err := rows.Scan(&a.ID, &a.BoardID, &a.Kind, &a.WhenStageID, &a.TargetStageID, &en, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Enabled = en != 0
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *kanbanStore) UpsertAutomation(ctx context.Context, a kanbanAutomation) (kanbanAutomation, error) {
	if a.BoardID == "" || a.Kind == "" || a.TargetStageID == "" {
		return kanbanAutomation{}, errors.New("board, kind and target required")
	}
	en := 0
	if a.Enabled {
		en = 1
	}
	if a.ID == "" {
		a.ID = newID()
		a.CreatedAt = time.Now().Unix()
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO kanban_automations (id,board_id,kind,when_stage_id,target_stage_id,enabled,created_at)
			 VALUES (?,?,?,?,?,?,?)`,
			a.ID, a.BoardID, a.Kind, a.WhenStageID, a.TargetStageID, en, a.CreatedAt); err != nil {
			return kanbanAutomation{}, err
		}
		return a, nil
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE kanban_automations SET kind=?, when_stage_id=?, target_stage_id=?, enabled=? WHERE id=?`,
		a.Kind, a.WhenStageID, a.TargetStageID, en, a.ID); err != nil {
		return kanbanAutomation{}, err
	}
	return a, nil
}

func (s *kanbanStore) DeleteAutomation(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM kanban_automations WHERE id = ?`, id)
	return err
}

// listAutomationsByKind returns every enabled automation of the given kind
// across all boards. Used by the trigger helpers below.
func (s *kanbanStore) listAutomationsByKind(ctx context.Context, kind string) ([]kanbanAutomation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,board_id,kind,when_stage_id,target_stage_id,enabled,created_at
		 FROM kanban_automations WHERE kind = ? AND enabled = 1`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []kanbanAutomation{}
	for rows.Next() {
		var a kanbanAutomation
		var en int
		if err := rows.Scan(&a.ID, &a.BoardID, &a.Kind, &a.WhenStageID, &a.TargetStageID, &en, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Enabled = en != 0
		out = append(out, a)
	}
	return out, rows.Err()
}

// TriggerAutomation runs every enabled automation matching `kind` for the
// given (sessionID, chatJID). When a card already exists for that chat and
// sits in the rule's "when" stage (or has no stage tracked), it moves to the
// target stage. When no card exists and the rule's WhenStageID is empty
// (the "sem etapa" / new-contact case), a fresh card is created in the
// target stage. Failures are logged-only — never block message ingestion.
func (s *kanbanStore) TriggerAutomation(ctx context.Context, kind, sessionID, chatJID, title string) {
	if sessionID == "" || chatJID == "" {
		return
	}
	rules, err := s.listAutomationsByKind(ctx, kind)
	if err != nil || len(rules) == 0 {
		return
	}
	existing, _ := s.CardsByChat(ctx, sessionID, chatJID)
	for _, r := range rules {
		// Locate a card on this rule's board (cards live on their board).
		var card *kanbanCard
		for i := range existing {
			if existing[i].BoardID == r.BoardID {
				card = &existing[i]
				break
			}
		}
		if card == nil {
			// No card yet → only the empty "when" stage may bootstrap.
			if r.WhenStageID != "" {
				continue
			}
			displayTitle := strings.TrimSpace(title)
			if displayTitle == "" {
				displayTitle = chatJID
			}
			_, _ = s.CreateCard(ctx, cardCreate{
				BoardID: r.BoardID, ColumnID: r.TargetStageID,
				Title: displayTitle, SessionID: sessionID, ChatJID: chatJID,
			})
			continue
		}
		// Skip cards whose current column doesn't match the rule's "when"
		// stage (unless the rule targets "no stage", which matches anything).
		if r.WhenStageID != "" && card.ColumnID != r.WhenStageID {
			continue
		}
		if card.ColumnID == r.TargetStageID {
			continue
		}
		_ = s.MoveCard(ctx, card.ID, r.TargetStageID, 0)
	}
}
