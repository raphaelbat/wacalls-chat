package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// quickReplyRow is a reusable message snippet triggered in the composer by
// typing "/" followed by the shortcut. Content may embed variables such as
// {{nome}} or {{protocolo}} which are resolved client-side at insert time.
//
// A snippet may also carry ONE attachment (image, video, audio or document).
// The file lives under media/quickreplies/; MediaPath is its path relative to
// the media root and MediaURL is the browser-facing URL derived from it,
// served by the authenticated /api/media/ handler.
type quickReplyRow struct {
	ID        string `json:"id"`
	Shortcut  string `json:"shortcut"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Global    bool   `json:"global"`
	OwnerID   string `json:"ownerId,omitempty"`
	TenantID  string `json:"tenantId,omitempty"`
	UsedCount int64  `json:"usedCount"`
	CreatedAt int64  `json:"createdAt"`

	MediaPath string `json:"-"`
	MediaURL  string `json:"mediaUrl,omitempty"`
	MediaName string `json:"mediaName,omitempty"`
	MediaMime string `json:"mediaMime,omitempty"`
	MediaKind string `json:"mediaKind,omitempty"` // image | video | audio | document
	MediaSize int64  `json:"mediaSize,omitempty"`
}

// quickReplyColumns is the shared SELECT projection. COALESCE keeps rows
// created before the media columns existed readable.
const quickReplyColumns = `id, shortcut, title, content, is_global, owner_id, tenant_id, used_count, created_at,
		COALESCE(media_path, ''), COALESCE(media_name, ''), COALESCE(media_mime, ''), COALESCE(media_kind, ''), COALESCE(media_size, 0)`

type rowScanner interface{ Scan(dest ...any) error }

// scanQuickReply reads one row selected with quickReplyColumns and fills the
// derived fields (Global, MediaURL).
func scanQuickReply(sc rowScanner) (quickReplyRow, error) {
	var r quickReplyRow
	var g int
	if err := sc.Scan(&r.ID, &r.Shortcut, &r.Title, &r.Content, &g, &r.OwnerID, &r.TenantID,
		&r.UsedCount, &r.CreatedAt, &r.MediaPath, &r.MediaName, &r.MediaMime, &r.MediaKind, &r.MediaSize); err != nil {
		return quickReplyRow{}, err
	}
	r.Global = g == 1
	r.MediaURL = quickReplyMediaURL(r.MediaPath)
	return r, nil
}

// quickReplyMediaURL turns a stored relative path into the URL the SPA fetches.
// Paths are always stored with forward slashes so this works on Windows too.
func quickReplyMediaURL(path string) string {
	if path == "" {
		return ""
	}
	return "/api/media/" + strings.ReplaceAll(path, "\\", "/")
}

type quickReplyStore struct{ db *sql.DB }

var ErrQuickReplyNotFound = errors.New("quick reply not found")

func newQuickReplyStore(ctx context.Context, db *sql.DB) (*quickReplyStore, error) {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS quick_replies (
		id         TEXT PRIMARY KEY,
		shortcut   TEXT NOT NULL,
		title      TEXT NOT NULL DEFAULT '',
		content    TEXT NOT NULL DEFAULT '',
		is_global  INTEGER NOT NULL DEFAULT 1,
		owner_id   TEXT NOT NULL DEFAULT '',
		tenant_id  TEXT NOT NULL DEFAULT '',
		used_count INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL
	)`); err != nil {
		return nil, err
	}
	_, _ = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_quick_replies_tenant ON quick_replies (tenant_id, shortcut)`)

	// Migração das bases antigas: adiciona as colunas de mídia. O erro
	// "duplicate column name" na segunda execução é esperado e ignorado.
	for _, stmt := range []string{
		`ALTER TABLE quick_replies ADD COLUMN media_path TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE quick_replies ADD COLUMN media_name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE quick_replies ADD COLUMN media_mime TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE quick_replies ADD COLUMN media_kind TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE quick_replies ADD COLUMN media_size INTEGER NOT NULL DEFAULT 0`,
	} {
		_, _ = db.ExecContext(ctx, stmt)
	}
	return &quickReplyStore{db: db}, nil
}

func normalizeShortcut(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(s, "/")
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

// List returns every snippet visible to a user: tenant-wide (global) ones
// plus the user's personal snippets.
func (s *quickReplyStore) List(ctx context.Context, userID, tenantID string) ([]quickReplyRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+quickReplyColumns+`
		FROM quick_replies
		WHERE (tenant_id = ? AND is_global = 1) OR owner_id = ?
		ORDER BY used_count DESC, shortcut ASC`, tenantID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []quickReplyRow{}
	for rows.Next() {
		r, err := scanQuickReply(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *quickReplyStore) Create(ctx context.Context, in quickReplyRow) (quickReplyRow, error) {
	in.Shortcut = normalizeShortcut(in.Shortcut)
	in.Title = strings.TrimSpace(in.Title)
	in.Content = strings.TrimSpace(in.Content)
	if in.Shortcut == "" {
		return quickReplyRow{}, errors.New("shortcut required")
	}
	// O conteúdo pode ficar vazio quando o snippet for só mídia: o anexo é
	// enviado logo depois via POST /api/quick-replies/{id}/media.
	if in.Title == "" {
		in.Title = in.Shortcut
	}
	in.ID = newID()
	in.CreatedAt = time.Now().Unix()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO quick_replies
		(id, shortcut, title, content, is_global, owner_id, tenant_id, used_count, created_at)
		VALUES (?,?,?,?,?,?,?,0,?)`,
		in.ID, in.Shortcut, in.Title, in.Content, qBool(in.Global), in.OwnerID, in.TenantID, in.CreatedAt); err != nil {
		return quickReplyRow{}, err
	}
	return in, nil
}

func (s *quickReplyStore) Get(ctx context.Context, id string) (quickReplyRow, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+quickReplyColumns+` FROM quick_replies WHERE id = ?`, id)
	r, err := scanQuickReply(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return quickReplyRow{}, ErrQuickReplyNotFound
		}
		return quickReplyRow{}, err
	}
	return r, nil
}

// Update altera apenas os campos de texto — a mídia tem endpoints próprios.
func (s *quickReplyStore) Update(ctx context.Context, id string, in quickReplyRow) error {
	res, err := s.db.ExecContext(ctx, `UPDATE quick_replies SET shortcut = ?, title = ?, content = ?, is_global = ? WHERE id = ?`,
		normalizeShortcut(in.Shortcut), strings.TrimSpace(in.Title), strings.TrimSpace(in.Content), qBool(in.Global), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrQuickReplyNotFound
	}
	return nil
}

// SetMedia grava o anexo do snippet e devolve a linha atualizada.
func (s *quickReplyStore) SetMedia(ctx context.Context, id, path, name, mime, kind string, size int64) (quickReplyRow, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE quick_replies
		SET media_path = ?, media_name = ?, media_mime = ?, media_kind = ?, media_size = ?
		WHERE id = ?`, path, name, mime, kind, size, id)
	if err != nil {
		return quickReplyRow{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return quickReplyRow{}, ErrQuickReplyNotFound
	}
	return s.Get(ctx, id)
}

// ClearMedia limpa as colunas de mídia e devolve o caminho antigo para que o
// chamador possa apagar o arquivo do disco.
func (s *quickReplyStore) ClearMedia(ctx context.Context, id string) (string, error) {
	row, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE quick_replies
		SET media_path = '', media_name = '', media_mime = '', media_kind = '', media_size = 0
		WHERE id = ?`, id); err != nil {
		return "", err
	}
	return row.MediaPath, nil
}

func (s *quickReplyStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM quick_replies WHERE id = ?`, id)
	return err
}

func (s *quickReplyStore) BumpUsage(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE quick_replies SET used_count = used_count + 1 WHERE id = ?`, id)
	return err
}
