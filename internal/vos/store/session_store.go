package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"vos/internal/vos/domain"
)

type SessionStore interface {
	Close() error
	CreateSession(session *domain.Session) error
	DeleteSession(sessionID string) error
	GetSession(sessionID string) (*domain.Session, error)
	AppendEvent(event *domain.SessionEvent, nextStatus *domain.SessionStatus) (*domain.Session, error)
	ListEvents(sessionID string, afterSeq int, limit int) ([]*domain.SessionEvent, error)
	ListEventsByCallID(sessionID, callID string, limit int) ([]*domain.SessionEvent, error)
}

type SQLiteSessionStore struct {
	path string
	db   *sql.DB
}

const (
	sqliteBusyRetryDelay = 25 * time.Millisecond
	sqliteBusyMaxRetries = 200
)

func NewSQLiteSessionStore(path string) (*SQLiteSessionStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create session database directory: %w", err)
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	store := &SQLiteSessionStore{
		path: path,
		db:   db,
	}
	if err := store.initDB(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (store *SQLiteSessionStore) Close() error {
	return store.db.Close()
}

func (store *SQLiteSessionStore) CreateSession(session *domain.Session) error {
	if session == nil {
		return domain.ValidationError{Message: "session is required"}
	}
	session.Normalize()
	ctx := context.Background()

	err := retryOnSQLiteBusy(ctx, func() error {
		_, execErr := store.db.ExecContext(
			ctx,
			`INSERT INTO sessions (id, node_id, status, created_at, updated_at, last_seq)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			session.ID,
			session.NodeID,
			string(session.Status),
			formatSessionTime(session.CreatedAt),
			formatSessionTime(session.UpdatedAt),
			session.LastSeq,
		)
		return execErr
	})
	if err != nil {
		if isSQLiteConstraint(err) {
			return domain.DuplicateEntityError{Kind: "session", ID: session.ID}
		}
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (store *SQLiteSessionStore) DeleteSession(sessionID string) error {
	ctx := context.Background()
	var result sql.Result
	err := retryOnSQLiteBusy(ctx, func() error {
		value, execErr := store.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID)
		if execErr != nil {
			return execErr
		}
		result = value
		return nil
	})
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	if rowsAffected == 0 {
		return domain.SessionNotFoundError{SessionID: sessionID}
	}
	return nil
}

func (store *SQLiteSessionStore) GetSession(sessionID string) (*domain.Session, error) {
	ctx := context.Background()
	row := store.db.QueryRowContext(
		ctx,
		`SELECT id, node_id, status, created_at, updated_at, last_seq
		   FROM sessions
		  WHERE id = ?`,
		sessionID,
	)

	var (
		id        string
		nodeID    string
		statusRaw string
		createdAt string
		updatedAt string
		lastSeq   int
	)
	if err := row.Scan(&id, &nodeID, &statusRaw, &createdAt, &updatedAt, &lastSeq); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.SessionNotFoundError{SessionID: sessionID}
		}
		return nil, fmt.Errorf("get session: %w", err)
	}

	status, err := domain.ParseSessionStatus(statusRaw)
	if err != nil {
		return nil, err
	}
	created, err := parseSessionTime(createdAt)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	updated, err := parseSessionTime(updatedAt)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}

	session := &domain.Session{
		ID:        id,
		NodeID:    nodeID,
		Status:    status,
		CreatedAt: created,
		UpdatedAt: updated,
		LastSeq:   lastSeq,
	}
	session.Normalize()
	return session, nil
}

func (store *SQLiteSessionStore) AppendEvent(event *domain.SessionEvent, nextStatus *domain.SessionStatus) (*domain.Session, error) {
	if event == nil {
		return nil, domain.ValidationError{Message: "session event is required"}
	}
	event.Normalize()

	var updated *domain.Session
	err := store.withImmediateTx(context.Background(), func(conn *sql.Conn) error {
		session, err := store.getSessionTx(context.Background(), conn, event.SessionID)
		if err != nil {
			return err
		}

		nextSeq := session.LastSeq + 1
		if event.Seq > 0 && event.Seq != nextSeq {
			return domain.SessionSequenceConflictError{
				SessionID: session.ID,
				Expected:  nextSeq,
				Actual:    event.Seq,
			}
		}
		event.Seq = nextSeq

		payload, err := json.Marshal(event.PayloadJSON)
		if err != nil {
			return fmt.Errorf("append session event: %w", err)
		}

		var providerItemID any
		if event.ProviderItemID != nil {
			providerItemID = *event.ProviderItemID
		}
		var role any
		if event.Role != nil {
			role = string(*event.Role)
		}
		var callID any
		if event.CallID != nil {
			callID = *event.CallID
		}

		if _, err := conn.ExecContext(
			context.Background(),
			`INSERT INTO session_events (id, session_id, seq, item_type, provider_item_id, role, call_id, payload_json, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			event.ID,
			event.SessionID,
			event.Seq,
			event.ItemType,
			providerItemID,
			role,
			callID,
			string(payload),
			formatSessionTime(event.CreatedAt),
		); err != nil {
			if isSQLiteConstraint(err) {
				return domain.DuplicateEntityError{Kind: "session_event", ID: event.ID}
			}
			return fmt.Errorf("append session event: %w", err)
		}

		session.LastSeq = event.Seq
		session.UpdatedAt = event.CreatedAt
		if nextStatus != nil {
			session.Status = *nextStatus
		}
		if _, err := conn.ExecContext(
			context.Background(),
			`UPDATE sessions
			    SET status = ?, updated_at = ?, last_seq = ?
			  WHERE id = ?`,
			string(session.Status),
			formatSessionTime(session.UpdatedAt),
			session.LastSeq,
			session.ID,
		); err != nil {
			return fmt.Errorf("append session event: %w", err)
		}

		updated = session
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (store *SQLiteSessionStore) ListEvents(sessionID string, afterSeq int, limit int) ([]*domain.SessionEvent, error) {
	if limit <= 0 {
		limit = 100
	}

	ctx := context.Background()
	if _, err := store.GetSession(sessionID); err != nil {
		return nil, err
	}

	rows, err := store.db.QueryContext(
		ctx,
		`SELECT id, session_id, seq, item_type, provider_item_id, role, call_id, payload_json, created_at
		   FROM session_events
		  WHERE session_id = ? AND seq > ?
		  ORDER BY seq ASC
		  LIMIT ?`,
		sessionID,
		afterSeq,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list session events: %w", err)
	}
	defer rows.Close()

	events := make([]*domain.SessionEvent, 0)
	for rows.Next() {
		event, err := scanSessionEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list session events: %w", err)
	}
	return events, nil
}

func (store *SQLiteSessionStore) ListEventsByCallID(sessionID, callID string, limit int) ([]*domain.SessionEvent, error) {
	if limit <= 0 {
		limit = 100
	}

	ctx := context.Background()
	if _, err := store.GetSession(sessionID); err != nil {
		return nil, err
	}

	rows, err := store.db.QueryContext(
		ctx,
		`SELECT id, session_id, seq, item_type, provider_item_id, role, call_id, payload_json, created_at
		   FROM session_events
		  WHERE session_id = ? AND call_id = ?
		  ORDER BY seq ASC
		  LIMIT ?`,
		sessionID,
		callID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list session events by call ID: %w", err)
	}
	defer rows.Close()

	events := make([]*domain.SessionEvent, 0)
	for rows.Next() {
		event, err := scanSessionEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list session events by call ID: %w", err)
	}
	return events, nil
}

func (store *SQLiteSessionStore) initDB(ctx context.Context) error {
	conn, err := store.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA busy_timeout = 5000`); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA journal_mode = WAL`); err != nil {
		return err
	}
	if err := ensureNoLegacySessionSchema(ctx, conn); err != nil {
		return err
	}
	if err := migrateSessionEventsItemTypeConstraint(ctx, conn); err != nil {
		return err
	}

	statements := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
		   id TEXT PRIMARY KEY,
		   node_id TEXT NOT NULL,
		   status TEXT NOT NULL CHECK (status IN ('active', 'waiting', 'completed', 'failed')),
		   created_at TEXT NOT NULL,
		   updated_at TEXT NOT NULL,
		   last_seq INTEGER NOT NULL CHECK (last_seq >= 0)
		 )`,
		`CREATE TABLE IF NOT EXISTS session_events (
		   id TEXT PRIMARY KEY,
		   session_id TEXT NOT NULL,
		   seq INTEGER NOT NULL CHECK (seq > 0),
		   item_type TEXT NOT NULL CHECK (length(trim(item_type)) > 0),
		   provider_item_id TEXT,
		   role TEXT CHECK (role IN ('user', 'assistant', 'tool', 'system')),
		   call_id TEXT,
		   payload_json TEXT NOT NULL,
		   created_at TEXT NOT NULL,
		   FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE,
		   UNIQUE (session_id, seq)
		 )`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_node_id_created_at
		    ON sessions(node_id, created_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_session_events_session_id_seq
		    ON session_events(session_id, seq ASC)`,
		`CREATE INDEX IF NOT EXISTS idx_session_events_session_id_call_id_seq
		    ON session_events(session_id, call_id, seq ASC)`,
	}

	for _, statement := range statements {
		if err := retryOnSQLiteBusy(ctx, func() error {
			_, execErr := conn.ExecContext(ctx, statement)
			return execErr
		}); err != nil {
			return err
		}
	}
	return nil
}

func ensureNoLegacySessionSchema(ctx context.Context, conn *sql.Conn) error {
	sessionTableExists, err := tableExists(ctx, conn, "sessions")
	if err != nil {
		return err
	}
	eventTableExists, err := tableExists(ctx, conn, "session_events")
	if err != nil {
		return err
	}
	if !sessionTableExists || !eventTableExists {
		return nil
	}

	hasKind, err := tableHasColumn(ctx, conn, "session_events", "kind")
	if err != nil {
		return err
	}
	if hasKind {
		return domain.ValidationError{Message: "legacy session_events.kind schema is not supported; recreate or manually migrate .vos_sessions.db"}
	}
	hasItemType, err := tableHasColumn(ctx, conn, "session_events", "item_type")
	if err != nil {
		return err
	}
	hasProviderItemID, err := tableHasColumn(ctx, conn, "session_events", "provider_item_id")
	if err != nil {
		return err
	}
	if !hasItemType || !hasProviderItemID {
		return domain.ValidationError{Message: "session_events schema is incompatible; recreate or manually migrate .vos_sessions.db"}
	}

	sessionsSQL, err := tableDefinitionSQL(ctx, conn, "sessions")
	if err != nil {
		return err
	}
	normalized := strings.ReplaceAll(strings.ToLower(sessionsSQL), " ", "")
	if strings.Contains(normalized, "('open','closed','failed')") {
		return domain.ValidationError{Message: "legacy sessions.status(open/closed) schema is not supported; recreate or manually migrate .vos_sessions.db"}
	}

	var hasLegacyStatusRows bool
	row := conn.QueryRowContext(
		ctx,
		`SELECT EXISTS(
			SELECT 1 FROM sessions WHERE status IN ('open', 'closed')
		)`,
	)
	if err := row.Scan(&hasLegacyStatusRows); err != nil {
		return err
	}
	if hasLegacyStatusRows {
		return domain.ValidationError{Message: "legacy session status values open/closed are not supported; please migrate data in .vos_sessions.db"}
	}

	var hasInvalidItemTypeRows bool
	row = conn.QueryRowContext(
		ctx,
		`SELECT EXISTS(
			SELECT 1
			  FROM session_events
			 WHERE item_type IS NULL OR length(trim(item_type)) = 0
		)`,
	)
	if err := row.Scan(&hasInvalidItemTypeRows); err != nil {
		return err
	}
	if hasInvalidItemTypeRows {
		return domain.ValidationError{Message: "session_events contains invalid item_type values; item_type must be a non-empty string"}
	}
	return nil
}

func migrateSessionEventsItemTypeConstraint(ctx context.Context, conn *sql.Conn) error {
	eventTableExists, err := tableExists(ctx, conn, "session_events")
	if err != nil {
		return err
	}
	if !eventTableExists {
		return nil
	}

	tableSQL, err := tableDefinitionSQL(ctx, conn, "session_events")
	if err != nil {
		return err
	}
	normalized := strings.ReplaceAll(strings.ToLower(tableSQL), " ", "")
	legacyConstraint := "item_typein('function_call','function_call_output')"
	if !strings.Contains(normalized, legacyConstraint) {
		return nil
	}

	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()

	statements := []string{
		`CREATE TABLE session_events_new (
		   id TEXT PRIMARY KEY,
		   session_id TEXT NOT NULL,
		   seq INTEGER NOT NULL CHECK (seq > 0),
		   item_type TEXT NOT NULL CHECK (length(trim(item_type)) > 0),
		   provider_item_id TEXT,
		   role TEXT CHECK (role IN ('user', 'assistant', 'tool', 'system')),
		   call_id TEXT,
		   payload_json TEXT NOT NULL,
		   created_at TEXT NOT NULL,
		   FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE,
		   UNIQUE (session_id, seq)
		 )`,
		`INSERT INTO session_events_new (id, session_id, seq, item_type, provider_item_id, role, call_id, payload_json, created_at)
		 SELECT id, session_id, seq, item_type, provider_item_id, role, call_id, payload_json, created_at
		   FROM session_events`,
		`DROP TABLE session_events`,
		`ALTER TABLE session_events_new RENAME TO session_events`,
	}

	for _, statement := range statements {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return err
		}
	}

	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return err
	}
	committed = true
	return nil
}

func tableExists(ctx context.Context, conn *sql.Conn, table string) (bool, error) {
	row := conn.QueryRowContext(
		ctx,
		`SELECT EXISTS(
			SELECT 1 FROM sqlite_master WHERE type='table' AND name = ?
		)`,
		table,
	)
	var exists bool
	if err := row.Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func tableHasColumn(ctx context.Context, conn *sql.Conn, table, column string) (bool, error) {
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func tableDefinitionSQL(ctx context.Context, conn *sql.Conn, table string) (string, error) {
	row := conn.QueryRowContext(
		ctx,
		`SELECT COALESCE(sql, '')
		   FROM sqlite_master
		  WHERE type = 'table' AND name = ?`,
		table,
	)
	var value string
	if err := row.Scan(&value); err != nil {
		return "", err
	}
	return value, nil
}

func (store *SQLiteSessionStore) withImmediateTx(ctx context.Context, fn func(conn *sql.Conn) error) error {
	return retryOnSQLiteBusy(ctx, func() error {
		return store.withImmediateTxOnce(ctx, fn)
	})
}

func (store *SQLiteSessionStore) withImmediateTxOnce(ctx context.Context, fn func(conn *sql.Conn) error) error {
	conn, err := store.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA busy_timeout = 5000`); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}

	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()

	if err := fn(conn); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return err
	}
	committed = true
	return nil
}

func (store *SQLiteSessionStore) getSessionTx(ctx context.Context, conn *sql.Conn, sessionID string) (*domain.Session, error) {
	row := conn.QueryRowContext(
		ctx,
		`SELECT id, node_id, status, created_at, updated_at, last_seq
		   FROM sessions
		  WHERE id = ?`,
		sessionID,
	)

	var (
		id        string
		nodeID    string
		statusRaw string
		createdAt string
		updatedAt string
		lastSeq   int
	)
	if err := row.Scan(&id, &nodeID, &statusRaw, &createdAt, &updatedAt, &lastSeq); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.SessionNotFoundError{SessionID: sessionID}
		}
		return nil, fmt.Errorf("get session: %w", err)
	}

	status, err := domain.ParseSessionStatus(statusRaw)
	if err != nil {
		return nil, err
	}
	created, err := parseSessionTime(createdAt)
	if err != nil {
		return nil, err
	}
	updated, err := parseSessionTime(updatedAt)
	if err != nil {
		return nil, err
	}

	return &domain.Session{
		ID:        id,
		NodeID:    nodeID,
		Status:    status,
		CreatedAt: created,
		UpdatedAt: updated,
		LastSeq:   lastSeq,
	}, nil
}

func scanSessionEvent(scanner interface {
	Scan(dest ...any) error
}) (*domain.SessionEvent, error) {
	var (
		id                string
		sessionID         string
		seq               int
		itemTypeRaw       string
		providerItemIDRaw sql.NullString
		roleRaw           sql.NullString
		callIDRaw         sql.NullString
		payloadRaw        string
		createdRaw        string
	)
	if err := scanner.Scan(
		&id,
		&sessionID,
		&seq,
		&itemTypeRaw,
		&providerItemIDRaw,
		&roleRaw,
		&callIDRaw,
		&payloadRaw,
		&createdRaw,
	); err != nil {
		return nil, fmt.Errorf("scan session event: %w", err)
	}

	itemType, err := domain.ParseSessionItemType(itemTypeRaw)
	if err != nil {
		return nil, err
	}

	var providerItemID *string
	if providerItemIDRaw.Valid {
		value := providerItemIDRaw.String
		providerItemID = &value
	}

	var role *domain.SessionRole
	if roleRaw.Valid {
		parsedRole, err := domain.ParseSessionRole(roleRaw.String)
		if err != nil {
			return nil, err
		}
		role = &parsedRole
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadRaw), &payload); err != nil {
		return nil, fmt.Errorf("scan session event: %w", err)
	}
	createdAt, err := parseSessionTime(createdRaw)
	if err != nil {
		return nil, fmt.Errorf("scan session event: %w", err)
	}

	var callID *string
	if callIDRaw.Valid {
		value := callIDRaw.String
		callID = &value
	}

	event := &domain.SessionEvent{
		ID:             id,
		SessionID:      sessionID,
		Seq:            seq,
		ItemType:       itemType,
		ProviderItemID: providerItemID,
		Role:           role,
		CallID:         callID,
		PayloadJSON:    payload,
		CreatedAt:      createdAt,
	}
	event.Normalize()
	return event, nil
}

func formatSessionTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseSessionTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}

func retryOnSQLiteBusy(ctx context.Context, fn func() error) error {
	var err error
	for attempt := 0; attempt < sqliteBusyMaxRetries; attempt++ {
		err = fn()
		if !isSQLiteBusy(err) {
			return err
		}
		if waitErr := waitWithContext(ctx, sqliteBusyRetryDelay); waitErr != nil {
			return waitErr
		}
	}
	return err
}

func waitWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "database is locked") || strings.Contains(message, "database is busy")
}

func isSQLiteConstraint(err error) bool {
	if err == nil {
		return false
	}
	return !errors.Is(err, sql.ErrNoRows) &&
		(strings.Contains(err.Error(), "UNIQUE constraint failed") || strings.Contains(err.Error(), "constraint failed"))
}
