package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// Campanhas de mídia: uma mensagem (texto e/ou arquivo) enviada para uma lista
// de contatos, com ritmo controlado.
//
// A lista de destinatários é uma FOTOGRAFIA tirada na hora de montar a
// campanha, não uma consulta viva. Isso é de propósito: uma campanha que cresce
// sozinha porque alguém etiquetou mais um contato é a receita para disparar
// para quem você não pretendia.

type CampaignRow struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"` // draft | running | paused | finished
	OwnerID string `json:"-"`

	// Conteúdo da mensagem.
	Text      string `json:"text"`
	MediaURL  string `json:"mediaUrl"`
	MediaKind string `json:"mediaKind"` // image | video | audio | document
	Filename  string `json:"filename"`

	// Números usados no rodízio, separados por vírgula. Vazio = todos os
	// conectados no momento do disparo.
	SessionIDs string `json:"sessionIds"`

	// Ritmo.
	MinIntervalSec int `json:"minIntervalSec"`
	MaxIntervalSec int `json:"maxIntervalSec"`
	PerHour        int `json:"perHour"` // por número, 0 = sem teto
	PerDay         int `json:"perDay"`  // por número, 0 = sem teto

	// Janela permitida, em hora local do servidor. Fora dela a campanha
	// dorme em vez de terminar — ninguém quer propaganda às 3 da manhã.
	WindowStart int    `json:"windowStart"` // 0..23
	WindowEnd   int    `json:"windowEnd"`   // 0..23, exclusivo
	Weekdays    string `json:"weekdays"`    // "1,2,3,4,5" (0=domingo)

	// Aquecimento: número novo começa devagar e vai soltando ao longo dos dias.
	Warmup bool `json:"warmup"`

	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt int64  `json:"finishedAt"`
	LastError  string `json:"lastError"`
}

type CampaignTarget struct {
	ID         int64  `json:"id"`
	CampaignID string `json:"campaignId"`
	JID        string `json:"jid"`
	Name       string `json:"name"`
	Status     string `json:"status"` // pending | sent | failed | skipped
	SessionID  string `json:"sessionId"`
	Error      string `json:"error"`
	SentAt     int64  `json:"sentAt"`
	Attempts   int    `json:"attempts"`
}

type CampaignProgress struct {
	Total   int `json:"total"`
	Pending int `json:"pending"`
	Sent    int `json:"sent"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

type campaignStore struct{ db *sql.DB }

func newCampaignStore(ctx context.Context, db *sql.DB) (*campaignStore, error) {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS campaigns (
			id               TEXT PRIMARY KEY,
			owner_id         TEXT NOT NULL DEFAULT '',
			name             TEXT NOT NULL,
			status           TEXT NOT NULL DEFAULT 'draft',
			text             TEXT NOT NULL DEFAULT '',
			media_url        TEXT NOT NULL DEFAULT '',
			media_kind       TEXT NOT NULL DEFAULT '',
			filename         TEXT NOT NULL DEFAULT '',
			session_ids      TEXT NOT NULL DEFAULT '',
			min_interval_sec INTEGER NOT NULL DEFAULT 25,
			max_interval_sec INTEGER NOT NULL DEFAULT 70,
			per_hour         INTEGER NOT NULL DEFAULT 40,
			per_day          INTEGER NOT NULL DEFAULT 250,
			window_start     INTEGER NOT NULL DEFAULT 8,
			window_end       INTEGER NOT NULL DEFAULT 20,
			weekdays         TEXT NOT NULL DEFAULT '1,2,3,4,5,6',
			warmup           INTEGER NOT NULL DEFAULT 1,
			created_at       INTEGER NOT NULL,
			updated_at       INTEGER NOT NULL,
			started_at       INTEGER NOT NULL DEFAULT 0,
			finished_at      INTEGER NOT NULL DEFAULT 0,
			last_error       TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS campaign_targets (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			campaign_id TEXT NOT NULL,
			jid         TEXT NOT NULL,
			name        TEXT NOT NULL DEFAULT '',
			status      TEXT NOT NULL DEFAULT 'pending',
			session_id  TEXT NOT NULL DEFAULT '',
			error       TEXT NOT NULL DEFAULT '',
			sent_at     INTEGER NOT NULL DEFAULT 0,
			attempts    INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY(campaign_id) REFERENCES campaigns(id) ON DELETE CASCADE
		)`,
		// Um contato só entra uma vez na mesma campanha. Sem isto, importar a
		// planilha duas vezes dobra o disparo para todo mundo.
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_campaign_targets_unico
			ON campaign_targets (campaign_id, jid)`,
		`CREATE INDEX IF NOT EXISTS idx_campaign_targets_fila
			ON campaign_targets (campaign_id, status)`,
		// Livro-caixa do que cada número já disparou. É o que sustenta o teto
		// por hora e por dia mesmo depois de reiniciar o servidor.
		`CREATE TABLE IF NOT EXISTS campaign_sends (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			ts         INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_campaign_sends_sessao
			ON campaign_sends (session_id, ts)`,
	}
	for _, q := range stmts {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return nil, err
		}
	}
	return &campaignStore{db: db}, nil
}

func novoIDCampanha() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "camp_" + hex.EncodeToString(b)
}

var errCampanhaNaoEncontrada = errors.New("campanha não encontrada")

const colunasCampanha = `id, COALESCE(owner_id,''), name, status, text, media_url, media_kind, filename,
	session_ids, min_interval_sec, max_interval_sec, per_hour, per_day,
	window_start, window_end, weekdays, warmup, created_at, updated_at,
	started_at, finished_at, COALESCE(last_error,'')`

func lerCampanha(sc interface{ Scan(...any) error }) (CampaignRow, error) {
	var c CampaignRow
	var warmup int
	err := sc.Scan(&c.ID, &c.OwnerID, &c.Name, &c.Status, &c.Text, &c.MediaURL, &c.MediaKind, &c.Filename,
		&c.SessionIDs, &c.MinIntervalSec, &c.MaxIntervalSec, &c.PerHour, &c.PerDay,
		&c.WindowStart, &c.WindowEnd, &c.Weekdays, &warmup, &c.CreatedAt, &c.UpdatedAt,
		&c.StartedAt, &c.FinishedAt, &c.LastError)
	c.Warmup = warmup == 1
	return c, err
}

func (s *campaignStore) List(ctx context.Context, ownerID string, admin bool) ([]CampaignRow, error) {
	q := `SELECT ` + colunasCampanha + ` FROM campaigns ORDER BY updated_at DESC`
	var rows *sql.Rows
	var err error
	if admin {
		rows, err = s.db.QueryContext(ctx, q)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT `+colunasCampanha+` FROM campaigns WHERE owner_id = ? ORDER BY updated_at DESC`, ownerID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CampaignRow{}
	for rows.Next() {
		c, err := lerCampanha(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *campaignStore) Get(ctx context.Context, id string) (*CampaignRow, error) {
	c, err := lerCampanha(s.db.QueryRowContext(ctx, `SELECT `+colunasCampanha+` FROM campaigns WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errCampanhaNaoEncontrada
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

/** Campanhas que o disparador precisa olhar. */
func (s *campaignStore) Rodando(ctx context.Context) ([]CampaignRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+colunasCampanha+` FROM campaigns WHERE status = 'running' ORDER BY started_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CampaignRow{}
	for rows.Next() {
		c, err := lerCampanha(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *campaignStore) Create(ctx context.Context, c *CampaignRow) error {
	agora := time.Now().Unix()
	c.ID = novoIDCampanha()
	c.CreatedAt, c.UpdatedAt = agora, agora
	if c.Status == "" {
		c.Status = "draft"
	}
	warmup := 0
	if c.Warmup {
		warmup = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO campaigns
		(id, owner_id, name, status, text, media_url, media_kind, filename, session_ids,
		 min_interval_sec, max_interval_sec, per_hour, per_day, window_start, window_end,
		 weekdays, warmup, created_at, updated_at, started_at, finished_at, last_error)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,0,0,'')`,
		c.ID, c.OwnerID, c.Name, c.Status, c.Text, c.MediaURL, c.MediaKind, c.Filename, c.SessionIDs,
		c.MinIntervalSec, c.MaxIntervalSec, c.PerHour, c.PerDay, c.WindowStart, c.WindowEnd,
		c.Weekdays, warmup, c.CreatedAt, c.UpdatedAt)
	return err
}

func (s *campaignStore) Update(ctx context.Context, c *CampaignRow) error {
	c.UpdatedAt = time.Now().Unix()
	warmup := 0
	if c.Warmup {
		warmup = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE campaigns SET
		name=?, status=?, text=?, media_url=?, media_kind=?, filename=?, session_ids=?,
		min_interval_sec=?, max_interval_sec=?, per_hour=?, per_day=?, window_start=?, window_end=?,
		weekdays=?, warmup=?, updated_at=?, started_at=?, finished_at=?, last_error=?
		WHERE id=?`,
		c.Name, c.Status, c.Text, c.MediaURL, c.MediaKind, c.Filename, c.SessionIDs,
		c.MinIntervalSec, c.MaxIntervalSec, c.PerHour, c.PerDay, c.WindowStart, c.WindowEnd,
		c.Weekdays, warmup, c.UpdatedAt, c.StartedAt, c.FinishedAt, c.LastError, c.ID)
	return err
}

func (s *campaignStore) SetStatus(ctx context.Context, id, status, motivo string) error {
	agora := time.Now().Unix()
	campos := `status=?, updated_at=?, last_error=?`
	args := []any{status, agora, motivo}
	if status == "running" {
		campos += `, started_at=CASE WHEN started_at=0 THEN ? ELSE started_at END`
		args = append(args, agora)
	}
	if status == "finished" {
		campos += `, finished_at=?`
		args = append(args, agora)
	}
	args = append(args, id)
	_, err := s.db.ExecContext(ctx, `UPDATE campaigns SET `+campos+` WHERE id=?`, args...)
	return err
}

func (s *campaignStore) Delete(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM campaign_targets WHERE campaign_id=?`, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM campaigns WHERE id=?`, id)
	return err
}

// ---------------------------------------------------------------------------
// Destinatários
// ---------------------------------------------------------------------------

// AddTargets grava a lista ignorando repetido (mesmo contato na mesma
// campanha). Devolve quantos entraram de fato.
func (s *campaignStore) AddTargets(ctx context.Context, campaignID string, alvos []CampaignTarget) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	st, err := tx.PrepareContext(ctx,
		`INSERT OR IGNORE INTO campaign_targets (campaign_id, jid, name, status) VALUES (?,?,?,'pending')`)
	if err != nil {
		return 0, err
	}
	defer st.Close()

	inseridos := 0
	for _, a := range alvos {
		jid := strings.TrimSpace(a.JID)
		if jid == "" {
			continue
		}
		res, err := st.ExecContext(ctx, campaignID, jid, strings.TrimSpace(a.Name))
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			inseridos++
		}
	}
	return inseridos, tx.Commit()
}

func (s *campaignStore) Targets(ctx context.Context, campaignID, status string, limite int) ([]CampaignTarget, error) {
	q := `SELECT id, campaign_id, jid, name, status, session_id, COALESCE(error,''), sent_at, attempts
	      FROM campaign_targets WHERE campaign_id=?`
	args := []any{campaignID}
	if status != "" {
		q += ` AND status=?`
		args = append(args, status)
	}
	q += ` ORDER BY id`
	if limite > 0 {
		q += ` LIMIT ?`
		args = append(args, limite)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CampaignTarget{}
	for rows.Next() {
		var t CampaignTarget
		if err := rows.Scan(&t.ID, &t.CampaignID, &t.JID, &t.Name, &t.Status, &t.SessionID, &t.Error, &t.SentAt, &t.Attempts); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *campaignStore) RemoveTargets(ctx context.Context, campaignID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM campaign_targets WHERE campaign_id=?`, campaignID)
	return err
}

/** Próximo da fila. Devolve nil quando não há mais ninguém pendente. */
func (s *campaignStore) ProximoPendente(ctx context.Context, campaignID string) (*CampaignTarget, error) {
	alvos, err := s.Targets(ctx, campaignID, "pending", 1)
	if err != nil || len(alvos) == 0 {
		return nil, err
	}
	return &alvos[0], nil
}

func (s *campaignStore) MarcarAlvo(ctx context.Context, id int64, status, sessionID, erro string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE campaign_targets SET status=?, session_id=?, error=?, sent_at=?, attempts=attempts+1 WHERE id=?`,
		status, sessionID, erro, time.Now().Unix(), id)
	return err
}

func (s *campaignStore) Progresso(ctx context.Context, campaignID string) (CampaignProgress, error) {
	var p CampaignProgress
	rows, err := s.db.QueryContext(ctx,
		`SELECT status, COUNT(1) FROM campaign_targets WHERE campaign_id=? GROUP BY status`, campaignID)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return p, err
		}
		p.Total += n
		switch st {
		case "pending":
			p.Pending = n
		case "sent":
			p.Sent = n
		case "failed":
			p.Failed = n
		case "skipped":
			p.Skipped = n
		}
	}
	return p, rows.Err()
}

// ---------------------------------------------------------------------------
// Livro-caixa dos envios (é o que faz o teto sobreviver a um restart)
// ---------------------------------------------------------------------------

func (s *campaignStore) RegistrarEnvio(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO campaign_sends (session_id, ts) VALUES (?,?)`,
		sessionID, time.Now().Unix())
	return err
}

/** Quantos aquele número disparou desde `desde`. */
func (s *campaignStore) EnviosDesde(ctx context.Context, sessionID string, desde int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM campaign_sends WHERE session_id=? AND ts >= ?`, sessionID, desde).Scan(&n)
	return n, err
}

/** Quando aquele número disparou pela primeira vez — base do aquecimento. */
func (s *campaignStore) PrimeiroEnvio(ctx context.Context, sessionID string) (int64, error) {
	var ts sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT MIN(ts) FROM campaign_sends WHERE session_id=?`, sessionID).Scan(&ts)
	if err != nil || !ts.Valid {
		return 0, err
	}
	return ts.Int64, nil
}

/** Limpeza: o livro-caixa só precisa da última semana. */
func (s *campaignStore) LimparEnviosAntigos(ctx context.Context) error {
	corte := time.Now().Add(-8 * 24 * time.Hour).Unix()
	_, err := s.db.ExecContext(ctx, `DELETE FROM campaign_sends WHERE ts < ?`, corte)
	return err
}
