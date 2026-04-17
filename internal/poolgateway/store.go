package poolgateway

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	path string
	db   *sql.DB
}

const (
	sqliteBusyRetryDelay = 25 * time.Millisecond
	sqliteBusyMaxRetries = 200

	metaKeyGlobalMaxConcurrent     = "global_max_concurrent"
	metaKeyOfflineFailureThreshold = "offline_failure_threshold"
	metaKeyModelConfigHash         = "model_config_hash"
)

func NewStore(path string) (*Store, error) {
	cleanPath := filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(cleanPath), 0o755); err != nil {
		return nil, fmt.Errorf("create pool database directory: %w", err)
	}
	db, err := sql.Open("sqlite3", cleanPath)
	if err != nil {
		return nil, err
	}
	store := &Store{
		path: cleanPath,
		db:   db,
	}
	if err := store.initDB(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (store *Store) Close() error {
	return store.db.Close()
}

func (store *Store) SyncFromModelConfig(ctx context.Context, config ModelConfig) error {
	return store.withImmediateTx(ctx, func(conn *sql.Conn) error {
		return store.syncConfig(ctx, conn, config)
	})
}

func (store *Store) ReserveInvocation(
	ctx context.Context,
	config ModelConfig,
	request InvokeRequest,
) (InvocationReservation, error) {
	var reservation InvocationReservation
	err := store.withImmediateTx(ctx, func(conn *sql.Conn) error {
		if err := store.syncConfig(ctx, conn, config); err != nil {
			return err
		}
		value, err := store.reserveAttemptTx(ctx, conn, config, request, nil)
		if err != nil {
			return err
		}
		reservation = value
		return nil
	})
	return reservation, err
}

func (store *Store) ReserveRetryAttempt(
	ctx context.Context,
	config ModelConfig,
	invocationID string,
	request InvokeRequest,
) (InvocationReservation, error) {
	var reservation InvocationReservation
	err := store.withImmediateTx(ctx, func(conn *sql.Conn) error {
		if err := store.syncConfig(ctx, conn, config); err != nil {
			return err
		}
		value, err := store.reserveAttemptTx(ctx, conn, config, request, &invocationID)
		if err != nil {
			return err
		}
		reservation = value
		return nil
	})
	return reservation, err
}

func (store *Store) CompleteInvocationSuccess(
	ctx context.Context,
	reservation InvocationReservation,
	outputText *string,
	rawResponse map[string]any,
	usage *UsageMetrics,
	finishedAt time.Time,
) (InvokeResponse, error) {
	var response InvokeResponse
	err := store.withImmediateTx(ctx, func(conn *sql.Conn) error {
		row, err := store.getInvocationRuntimeRow(ctx, conn, reservation.InvocationID)
		if err != nil {
			return err
		}

		if row.LeaseCount <= 0 {
			return fmt.Errorf("lease count underflow on API: %s", reservation.APIID)
		}

		newLease := row.LeaseCount - 1
		nextStatus := resolveAPIStatus(row.Enabled != 0, newLease, row.MaxConcurrent, 0, store.getOfflineFailureThreshold(ctx, conn))
		usageJSON, err := marshalNullable(usage)
		if err != nil {
			return err
		}
		rawResponseJSON, err := marshalNullable(rawResponse)
		if err != nil {
			return err
		}

		if _, err := conn.ExecContext(
			ctx,
			`UPDATE invocation_attempts
			   SET status = ?, finished_at = ?, usage_json = ?, error_json = NULL
			 WHERE attempt_id = ?`,
			string(InvocationStatusSuccess),
			formatTime(finishedAt),
			usageJSON,
			reservation.AttemptID,
		); err != nil {
			return err
		}
		if _, err := conn.ExecContext(
			ctx,
			`UPDATE invocations
			   SET status = ?, finished_at = ?, output_text = ?, raw_response_json = ?,
			       usage_json = ?, error_json = NULL
			 WHERE invocation_id = ?`,
			string(InvocationStatusSuccess),
			formatTime(finishedAt),
			outputText,
			rawResponseJSON,
			usageJSON,
			reservation.InvocationID,
		); err != nil {
			return err
		}
		if _, err := conn.ExecContext(
			ctx,
			`UPDATE apis
			   SET lease_count = ?, failure_count = 0, last_error = NULL, status = ?
			 WHERE api_id = ?`,
			newLease,
			string(nextStatus),
			reservation.APIID,
		); err != nil {
			return err
		}

		value, err := store.loadInvocationResponseTx(ctx, conn, reservation.InvocationID)
		if err != nil {
			return err
		}
		response = value
		return nil
	})
	return response, err
}

func (store *Store) CompleteAttemptFailure(
	ctx context.Context,
	reservation InvocationReservation,
	gatewayError GatewayError,
	finishedAt time.Time,
) error {
	err := store.withImmediateTx(ctx, func(conn *sql.Conn) error {
		row, err := store.getInvocationRuntimeRow(ctx, conn, reservation.InvocationID)
		if err != nil {
			return err
		}

		if row.LeaseCount <= 0 {
			return fmt.Errorf("lease count underflow on API: %s", reservation.APIID)
		}

		newLease := row.LeaseCount - 1
		nextFailure := row.FailureCount
		if countsTowardOffline(gatewayError) {
			nextFailure++
		}
		threshold := store.getOfflineFailureThreshold(ctx, conn)
		nextStatus := resolveAPIStatus(row.Enabled != 0, newLease, row.MaxConcurrent, nextFailure, threshold)
		errorJSON, err := marshalNullable(gatewayError)
		if err != nil {
			return err
		}

		if _, err := conn.ExecContext(
			ctx,
			`UPDATE invocation_attempts
			   SET status = ?, finished_at = ?, usage_json = NULL, error_json = ?
			 WHERE attempt_id = ?`,
			string(InvocationStatusFailure),
			formatTime(finishedAt),
			errorJSON,
			reservation.AttemptID,
		); err != nil {
			return err
		}
		if _, err := conn.ExecContext(
			ctx,
			`UPDATE apis
			   SET lease_count = ?, failure_count = ?, last_error = ?, status = ?
			 WHERE api_id = ?`,
			newLease,
			nextFailure,
			gatewayError.Message,
			string(nextStatus),
			reservation.APIID,
		); err != nil {
			return err
		}
		return nil
	})
	return err
}

func (store *Store) CompleteInvocationFailure(
	ctx context.Context,
	invocationID string,
	gatewayError GatewayError,
	finishedAt time.Time,
) (InvokeResponse, error) {
	var response InvokeResponse
	err := store.withImmediateTx(ctx, func(conn *sql.Conn) error {
		errorJSON, err := marshalNullable(gatewayError)
		if err != nil {
			return err
		}
		if _, err := conn.ExecContext(
			ctx,
			`UPDATE invocations
			   SET status = ?, finished_at = ?, output_text = NULL, raw_response_json = NULL,
			       usage_json = NULL, error_json = ?
			 WHERE invocation_id = ?`,
			string(InvocationStatusFailure),
			formatTime(finishedAt),
			errorJSON,
			invocationID,
		); err != nil {
			return err
		}

		value, err := store.loadInvocationResponseTx(ctx, conn, invocationID)
		if err != nil {
			return err
		}
		response = value
		return nil
	})
	return response, err
}

func (store *Store) Capacity(ctx context.Context, config ModelConfig) (CapacitySnapshot, error) {
	var snapshot CapacitySnapshot
	err := store.withImmediateTx(ctx, func(conn *sql.Conn) error {
		if err := store.syncConfig(ctx, conn, config); err != nil {
			return err
		}
		value, err := store.capacityTx(ctx, conn)
		if err != nil {
			return err
		}
		snapshot = value
		return nil
	})
	return snapshot, err
}

func (store *Store) ListRecords(
	ctx context.Context,
	config ModelConfig,
	nodeID *string,
	limit *int,
) ([]InvocationRecord, error) {
	records := make([]InvocationRecord, 0)
	err := store.withImmediateTx(ctx, func(conn *sql.Conn) error {
		if err := store.syncConfig(ctx, conn, config); err != nil {
			return err
		}

		query := `SELECT invocation_id FROM invocations`
		args := make([]any, 0, 2)
		if nodeID != nil {
			query += ` WHERE node_id = ?`
			args = append(args, *nodeID)
		}
		query += ` ORDER BY started_at DESC, invocation_id DESC`
		if limit != nil {
			query += ` LIMIT ?`
			args = append(args, *limit)
		}

		rows, err := conn.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var invocationID string
			if err := rows.Scan(&invocationID); err != nil {
				return err
			}
			record, err := store.loadInvocationRecordTx(ctx, conn, invocationID)
			if err != nil {
				return err
			}
			records = append(records, record)
		}
		return rows.Err()
	})
	return records, err
}

func (store *Store) initDB(ctx context.Context) error {
	conn, err := store.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	statements := []string{
		`PRAGMA foreign_keys = ON`,
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA journal_mode = WAL`,
		`CREATE TABLE IF NOT EXISTS meta (
		   key TEXT PRIMARY KEY,
		   value TEXT NOT NULL
		 )`,
		`CREATE TABLE IF NOT EXISTS apis (
		   api_id TEXT PRIMARY KEY,
		   provider TEXT NOT NULL,
		   model_class TEXT NOT NULL,
		   base_url TEXT NOT NULL,
		   api_key TEXT NOT NULL,
		   max_concurrent INTEGER NOT NULL CHECK (max_concurrent > 0),
		   enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
		   status TEXT NOT NULL CHECK (status IN ('available', 'leased', 'offline')),
		   lease_count INTEGER NOT NULL DEFAULT 0 CHECK (lease_count >= 0),
		   failure_count INTEGER NOT NULL DEFAULT 0 CHECK (failure_count >= 0),
		   last_error TEXT
		 )`,
		`CREATE TABLE IF NOT EXISTS invocations (
		   invocation_id TEXT PRIMARY KEY,
		   request_id TEXT NOT NULL,
		   node_id TEXT NOT NULL,
		   api_id TEXT,
		   request_json TEXT NOT NULL,
		   status TEXT NOT NULL CHECK (status IN ('running', 'success', 'failure')),
		   started_at TEXT NOT NULL,
		   finished_at TEXT,
		   output_text TEXT,
		   raw_response_json TEXT,
		   usage_json TEXT,
		   error_json TEXT,
		   FOREIGN KEY (api_id) REFERENCES apis(api_id)
		 )`,
		`CREATE TABLE IF NOT EXISTS invocation_attempts (
		   id INTEGER PRIMARY KEY AUTOINCREMENT,
		   attempt_id TEXT NOT NULL UNIQUE,
		   invocation_id TEXT NOT NULL,
		   api_id TEXT NOT NULL,
		   status TEXT NOT NULL CHECK (status IN ('running', 'success', 'failure')),
		   started_at TEXT NOT NULL,
		   finished_at TEXT,
		   usage_json TEXT,
		   error_json TEXT,
		   FOREIGN KEY (invocation_id) REFERENCES invocations(invocation_id),
		   FOREIGN KEY (api_id) REFERENCES apis(api_id)
		 )`,
		`CREATE INDEX IF NOT EXISTS idx_invocations_node_id ON invocations(node_id)`,
		`CREATE INDEX IF NOT EXISTS idx_invocation_attempts_invocation_id ON invocation_attempts(invocation_id)`,
	}
	for _, statement := range statements {
		if err := retryOnSQLiteBusy(ctx, func() error {
			_, err := conn.ExecContext(ctx, statement)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

func (store *Store) withImmediateTx(ctx context.Context, fn func(conn *sql.Conn) error) error {
	return retryOnSQLiteBusy(ctx, func() error {
		return store.withImmediateTxOnce(ctx, fn)
	})
}

func (store *Store) withImmediateTxOnce(ctx context.Context, fn func(conn *sql.Conn) error) error {
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

func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "database is locked") || strings.Contains(message, "database is busy")
}

func (store *Store) syncConfig(ctx context.Context, conn *sql.Conn, config ModelConfig) error {
	configHash, err := modelConfigHash(config)
	if err != nil {
		return err
	}
	existingHash, err := store.getMeta(ctx, conn, metaKeyModelConfigHash)
	if err != nil {
		return err
	}
	if existingHash != nil && *existingHash == configHash {
		return nil
	}

	if err := store.setMeta(ctx, conn, metaKeyGlobalMaxConcurrent, nullableIntString(config.GlobalMaxConcurrent)); err != nil {
		return err
	}
	if err := store.setMeta(ctx, conn, metaKeyOfflineFailureThreshold, fmt.Sprintf("%d", config.OfflineFailureThreshold)); err != nil {
		return err
	}

	configured := make(map[string]struct{}, len(config.APIs))
	for _, endpoint := range config.APIs {
		configured[endpoint.APIID] = struct{}{}
		var leaseCount int
		var failureCount int
		err := conn.QueryRowContext(
			ctx,
			`SELECT lease_count, failure_count FROM apis WHERE api_id = ?`,
			endpoint.APIID,
		).Scan(&leaseCount, &failureCount)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if errors.Is(err, sql.ErrNoRows) {
			leaseCount = 0
			failureCount = 0
		}

		status := resolveAPIStatus(endpoint.Enabled, leaseCount, endpoint.MaxConcurrent, failureCount, config.OfflineFailureThreshold)
		if _, err := conn.ExecContext(
			ctx,
			`INSERT INTO apis (
			   api_id, provider, model_class, base_url, api_key, max_concurrent,
			   enabled, status, lease_count, failure_count, last_error
			 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(api_id) DO UPDATE SET
			   provider = excluded.provider,
			   model_class = excluded.model_class,
			   base_url = excluded.base_url,
			   api_key = excluded.api_key,
			   max_concurrent = excluded.max_concurrent,
			   enabled = excluded.enabled,
			   status = excluded.status`,
			endpoint.APIID,
			string(endpoint.Provider),
			endpoint.Model,
			endpoint.BaseURL,
			endpoint.APIKey,
			endpoint.MaxConcurrent,
			boolToInt(endpoint.Enabled),
			string(status),
			leaseCount,
			failureCount,
			nil,
		); err != nil {
			return err
		}
	}

	if len(configured) == 0 {
		if _, err := conn.ExecContext(ctx, `UPDATE apis SET enabled = 0, status = 'offline'`); err != nil {
			return err
		}
		return store.setMeta(ctx, conn, metaKeyModelConfigHash, configHash)
	}

	query := `UPDATE apis SET enabled = 0, status = 'offline' WHERE api_id NOT IN (` + placeholders(len(configured)) + `)`
	args := make([]any, 0, len(configured))
	for apiID := range configured {
		args = append(args, apiID)
	}
	if _, err := conn.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	return store.setMeta(ctx, conn, metaKeyModelConfigHash, configHash)
}

func (store *Store) reserveAttemptTx(
	ctx context.Context,
	conn *sql.Conn,
	config ModelConfig,
	request InvokeRequest,
	invocationID *string,
) (InvocationReservation, error) {
	globalLimit, err := store.getGlobalLimit(ctx, conn)
	if err != nil {
		return InvocationReservation{}, err
	}
	if globalLimit != nil {
		var activeCount int
		if invocationID == nil {
			if err := conn.QueryRowContext(
				ctx,
				`SELECT COUNT(*) FROM invocations WHERE status = ?`,
				string(InvocationStatusRunning),
			).Scan(&activeCount); err != nil {
				return InvocationReservation{}, err
			}
		} else {
			if err := conn.QueryRowContext(
				ctx,
				`SELECT COUNT(*) FROM invocations WHERE status = ? AND invocation_id != ?`,
				string(InvocationStatusRunning),
				*invocationID,
			).Scan(&activeCount); err != nil {
				return InvocationReservation{}, err
			}
		}
		if activeCount >= *globalLimit {
			return InvocationReservation{}, ErrGlobalQuota
		}
	}

	query := `SELECT api_id, provider, model_class, base_url, api_key, max_concurrent, lease_count
	            FROM apis
	           WHERE enabled = 1 AND status != 'offline' AND lease_count < max_concurrent`
	args := make([]any, 0, 1)
	if request.RoutePolicy.APIID != nil {
		query += ` AND api_id = ?`
		args = append(args, *request.RoutePolicy.APIID)
	}
	query += ` ORDER BY lease_count ASC, api_id ASC LIMIT 1`

	row := conn.QueryRowContext(ctx, query, args...)
	var apiID string
	var provider string
	var model string
	var baseURL string
	var apiKey string
	var maxConcurrent int
	var leaseCount int
	if err := row.Scan(&apiID, &provider, &model, &baseURL, &apiKey, &maxConcurrent, &leaseCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return InvocationReservation{}, ErrNoCapacity
		}
		return InvocationReservation{}, err
	}

	newLease := leaseCount + 1
	nextStatus := ApiStatusAvailable
	if newLease >= maxConcurrent {
		nextStatus = ApiStatusLeased
	}
	if _, err := conn.ExecContext(
		ctx,
		`UPDATE apis SET lease_count = ?, status = ? WHERE api_id = ?`,
		newLease,
		string(nextStatus),
		apiID,
	); err != nil {
		return InvocationReservation{}, err
	}

	startedAt := utcNow()
	currentInvocationID := newUUID()
	attemptID := newUUID()
	apiConfig, found := config.APIByID(apiID)
	if !found {
		return InvocationReservation{}, fmt.Errorf("api config not found: %s", apiID)
	}
	if invocationID == nil {
		requestJSON, err := marshalNullable(normalizeRequest(request))
		if err != nil {
			return InvocationReservation{}, err
		}
		if _, err := conn.ExecContext(
			ctx,
			`INSERT INTO invocations (
			   invocation_id, request_id, node_id, api_id, request_json, status, started_at
			 ) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			currentInvocationID,
			request.RequestID,
			request.NodeID,
			apiID,
			requestJSON,
			string(InvocationStatusRunning),
			formatTime(startedAt),
		); err != nil {
			return InvocationReservation{}, err
		}
	} else {
		currentInvocationID = *invocationID
		result, err := conn.ExecContext(
			ctx,
			`UPDATE invocations
			   SET api_id = ?, status = ?, finished_at = NULL, output_text = NULL,
			       raw_response_json = NULL, usage_json = NULL, error_json = NULL
			 WHERE invocation_id = ?`,
			apiID,
			string(InvocationStatusRunning),
			currentInvocationID,
		)
		if err != nil {
			return InvocationReservation{}, err
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return InvocationReservation{}, err
		}
		if rowsAffected == 0 {
			return InvocationReservation{}, fmt.Errorf("invocation not found: %s", currentInvocationID)
		}
	}
	if _, err := conn.ExecContext(
		ctx,
		`INSERT INTO invocation_attempts (
		   attempt_id, invocation_id, api_id, status, started_at
		 ) VALUES (?, ?, ?, ?, ?)`,
		attemptID,
		currentInvocationID,
		apiID,
		string(InvocationStatusRunning),
		formatTime(startedAt),
	); err != nil {
		return InvocationReservation{}, err
	}

	return InvocationReservation{
		InvocationID:    currentInvocationID,
		AttemptID:       attemptID,
		RequestID:       request.RequestID,
		NodeID:          request.NodeID,
		APIID:           apiID,
		Provider:        provider,
		APIMode:         apiConfig.APIMode,
		Model:           model,
		BaseURL:         baseURL,
		APIKey:          apiKey,
		Headers:         cloneStringMap(apiConfig.Headers),
		RequestDefaults: cloneMap(apiConfig.RequestDefaults),
		Pricing:         copyPricing(apiConfig.Pricing),
		StartedAt:       startedAt,
	}, nil
}

func (store *Store) getInvocationRuntimeRow(
	ctx context.Context,
	conn *sql.Conn,
	invocationID string,
) (runtimeRow, error) {
	row := conn.QueryRowContext(
		ctx,
		`SELECT i.invocation_id, i.api_id, a.enabled, a.lease_count, a.max_concurrent, a.failure_count
		   FROM invocations AS i
		   JOIN apis AS a ON a.api_id = i.api_id
		  WHERE i.invocation_id = ?`,
		invocationID,
	)
	value := runtimeRow{}
	if err := row.Scan(
		&value.InvocationID,
		&value.APIID,
		&value.Enabled,
		&value.LeaseCount,
		&value.MaxConcurrent,
		&value.FailureCount,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return runtimeRow{}, fmt.Errorf("invocation not found: %s", invocationID)
		}
		return runtimeRow{}, err
	}
	return value, nil
}

func (store *Store) loadInvocationRecordTx(
	ctx context.Context,
	conn *sql.Conn,
	invocationID string,
) (InvocationRecord, error) {
	row := conn.QueryRowContext(
		ctx,
		`SELECT
		   i.invocation_id,
		   i.request_json,
		   i.status,
		   i.api_id,
		   i.output_text,
		   i.raw_response_json,
		   i.usage_json,
		   i.error_json,
		   i.started_at,
		   i.finished_at,
		   a.provider,
		   a.model_class
		 FROM invocations AS i
		 LEFT JOIN apis AS a ON a.api_id = i.api_id
		 WHERE i.invocation_id = ?`,
		invocationID,
	)

	var requestJSON string
	var status string
	var apiID sql.NullString
	var outputText sql.NullString
	var rawResponseJSON sql.NullString
	var usageJSON sql.NullString
	var errorJSON sql.NullString
	var startedAt string
	var finishedAt sql.NullString
	var provider sql.NullString
	var model sql.NullString

	if err := row.Scan(
		&invocationID,
		&requestJSON,
		&status,
		&apiID,
		&outputText,
		&rawResponseJSON,
		&usageJSON,
		&errorJSON,
		&startedAt,
		&finishedAt,
		&provider,
		&model,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return InvocationRecord{}, fmt.Errorf("invocation not found: %s", invocationID)
		}
		return InvocationRecord{}, err
	}

	var request InvokeRequest
	if err := json.Unmarshal([]byte(requestJSON), &request); err != nil {
		return InvocationRecord{}, err
	}
	request = normalizeRequest(request)

	var route *RouteDecision
	if apiID.Valid && provider.Valid && model.Valid {
		route = &RouteDecision{
			APIID:    apiID.String,
			Provider: provider.String,
			Model:    model.String,
		}
	}

	attemptRows, err := conn.QueryContext(
		ctx,
		`SELECT
		   ia.attempt_id,
		   ia.api_id,
		   ia.status,
		   ia.started_at,
		   ia.finished_at,
		   ia.usage_json,
		   ia.error_json,
		   a.provider,
		   a.model_class
		 FROM invocation_attempts AS ia
		 LEFT JOIN apis AS a ON a.api_id = ia.api_id
		 WHERE ia.invocation_id = ?
		 ORDER BY ia.id ASC`,
		invocationID,
	)
	if err != nil {
		return InvocationRecord{}, err
	}
	defer attemptRows.Close()

	attempts := make([]InvocationAttempt, 0)
	for attemptRows.Next() {
		var attemptID string
		var attemptAPIID string
		var attemptStatus string
		var attemptStarted string
		var attemptFinished sql.NullString
		var attemptUsageJSON sql.NullString
		var attemptErrorJSON sql.NullString
		var attemptProvider sql.NullString
		var attemptModel sql.NullString
		if err := attemptRows.Scan(
			&attemptID,
			&attemptAPIID,
			&attemptStatus,
			&attemptStarted,
			&attemptFinished,
			&attemptUsageJSON,
			&attemptErrorJSON,
			&attemptProvider,
			&attemptModel,
		); err != nil {
			return InvocationRecord{}, err
		}
		parsedAttemptStarted, err := parseTime(attemptStarted)
		if err != nil {
			return InvocationRecord{}, err
		}
		parsedAttemptFinished, err := parseNullableTime(attemptFinished)
		if err != nil {
			return InvocationRecord{}, err
		}
		attemptUsage, err := parseNullableUsage(attemptUsageJSON)
		if err != nil {
			return InvocationRecord{}, err
		}
		attemptError, err := parseNullableError(attemptErrorJSON)
		if err != nil {
			return InvocationRecord{}, err
		}
		attempts = append(attempts, InvocationAttempt{
			AttemptID: attemptID,
			Route: RouteDecision{
				APIID:    attemptAPIID,
				Provider: attemptProvider.String,
				Model:    attemptModel.String,
			},
			Status: InvocationStatus(attemptStatus),
			Timing: InvocationTiming{
				StartedAt:  parsedAttemptStarted,
				FinishedAt: parsedAttemptFinished,
				LatencyMS:  calculateLatency(parsedAttemptStarted, parsedAttemptFinished, attemptUsage),
			},
			Usage: attemptUsage,
			Error: attemptError,
		})
	}
	if err := attemptRows.Err(); err != nil {
		return InvocationRecord{}, err
	}

	parsedStartedAt, err := parseTime(startedAt)
	if err != nil {
		return InvocationRecord{}, err
	}
	parsedFinishedAt, err := parseNullableTime(finishedAt)
	if err != nil {
		return InvocationRecord{}, err
	}
	usage, err := parseNullableUsage(usageJSON)
	if err != nil {
		return InvocationRecord{}, err
	}
	gatewayError, err := parseNullableError(errorJSON)
	if err != nil {
		return InvocationRecord{}, err
	}
	rawResponse, err := parseNullableMap(rawResponseJSON)
	if err != nil {
		return InvocationRecord{}, err
	}
	return InvocationRecord{
		InvocationID: invocationID,
		Request:      request,
		Status:       InvocationStatus(status),
		Route:        route,
		OutputText:   nullStringPtr(outputText),
		Response:     rawResponse,
		Usage:        usage,
		Timing: InvocationTiming{
			StartedAt:  parsedStartedAt,
			FinishedAt: parsedFinishedAt,
			LatencyMS:  calculateLatency(parsedStartedAt, parsedFinishedAt, usage),
		},
		Error:    gatewayError,
		Attempts: attempts,
	}, nil
}

func (store *Store) loadInvocationResponseTx(
	ctx context.Context,
	conn *sql.Conn,
	invocationID string,
) (InvokeResponse, error) {
	record, err := store.loadInvocationRecordTx(ctx, conn, invocationID)
	if err != nil {
		return InvokeResponse{}, err
	}
	return InvokeResponse{
		InvocationID: record.InvocationID,
		RequestID:    record.Request.RequestID,
		NodeID:       record.Request.NodeID,
		Status:       record.Status,
		Route:        record.Route,
		Response:     record.Response,
		OutputText:   record.OutputText,
		Usage:        record.Usage,
		Timing:       record.Timing,
		Error:        record.Error,
	}, nil
}

func (store *Store) capacityTx(ctx context.Context, conn *sql.Conn) (CapacitySnapshot, error) {
	row := conn.QueryRowContext(
		ctx,
		`SELECT
		   COUNT(*) AS total_apis,
		   COALESCE(SUM(max_concurrent), 0) AS total_slots,
		   COALESCE(SUM(lease_count), 0) AS leased_slots,
		   COALESCE(
		     SUM(
		       CASE
		         WHEN status != 'offline' THEN
		           CASE WHEN max_concurrent - lease_count > 0
		                THEN max_concurrent - lease_count
		                ELSE 0 END
		         ELSE 0
		       END
		     ), 0
		   ) AS available_slots,
		   COALESCE(SUM(CASE WHEN status = 'offline' THEN 1 ELSE 0 END), 0) AS offline_apis
		 FROM apis`,
	)
	var totalAPIs int
	var totalSlots int
	var leasedSlots int
	var availableSlots int
	var offlineAPIs int
	if err := row.Scan(&totalAPIs, &totalSlots, &leasedSlots, &availableSlots, &offlineAPIs); err != nil {
		return CapacitySnapshot{}, err
	}

	var activeCount int
	if err := conn.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM invocations WHERE status = ?`,
		string(InvocationStatusRunning),
	).Scan(&activeCount); err != nil {
		return CapacitySnapshot{}, err
	}
	globalLimit, err := store.getGlobalLimit(ctx, conn)
	if err != nil {
		return CapacitySnapshot{}, err
	}
	throttled := globalLimit != nil && activeCount >= *globalLimit
	return CapacitySnapshot{
		TotalAPIs:      totalAPIs,
		TotalSlots:     totalSlots,
		AvailableSlots: availableSlots,
		LeasedSlots:    leasedSlots,
		OfflineAPIs:    offlineAPIs,
		Throttled:      throttled,
		UpdatedAt:      utcNow(),
	}, nil
}

func (store *Store) setMeta(ctx context.Context, conn *sql.Conn, key string, value string) error {
	_, err := conn.ExecContext(
		ctx,
		`INSERT INTO meta(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key,
		value,
	)
	return err
}

func (store *Store) getMeta(ctx context.Context, conn *sql.Conn, key string) (*string, error) {
	row := conn.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key)
	var value string
	if err := row.Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &value, nil
}

func (store *Store) getGlobalLimit(ctx context.Context, conn *sql.Conn) (*int, error) {
	value, err := store.getMeta(ctx, conn, metaKeyGlobalMaxConcurrent)
	if err != nil {
		return nil, err
	}
	if value == nil || *value == "" {
		return nil, nil
	}
	parsed := 0
	if _, err := fmt.Sscanf(*value, "%d", &parsed); err != nil {
		return nil, err
	}
	return &parsed, nil
}

func (store *Store) getOfflineFailureThreshold(ctx context.Context, conn *sql.Conn) int {
	value, err := store.getMeta(ctx, conn, metaKeyOfflineFailureThreshold)
	if err != nil || value == nil || *value == "" {
		return 3
	}
	parsed := 3
	if _, err := fmt.Sscanf(*value, "%d", &parsed); err != nil {
		return 3
	}
	return parsed
}

type runtimeRow struct {
	InvocationID  string
	APIID         string
	Enabled       int
	LeaseCount    int
	MaxConcurrent int
	FailureCount  int
}

func resolveAPIStatus(enabled bool, leaseCount int, maxConcurrent int, failureCount int, threshold int) ApiStatus {
	if !enabled {
		return ApiStatusOffline
	}
	if failureCount >= threshold {
		return ApiStatusOffline
	}
	if leaseCount >= maxConcurrent {
		return ApiStatusLeased
	}
	return ApiStatusAvailable
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullableIntString(value *int) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *value)
}

func placeholders(count int) string {
	result := ""
	for idx := 0; idx < count; idx++ {
		if idx > 0 {
			result += ", "
		}
		result += "?"
	}
	return result
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}

func parseNullableTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	parsed, err := parseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func marshalNullable(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return string(payload), nil
}

func parseNullableUsage(value sql.NullString) (*UsageMetrics, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	var usage UsageMetrics
	if err := json.Unmarshal([]byte(value.String), &usage); err != nil {
		return nil, err
	}
	return &usage, nil
}

func parseNullableError(value sql.NullString) (*GatewayError, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	var gatewayError GatewayError
	if err := json.Unmarshal([]byte(value.String), &gatewayError); err != nil {
		return nil, err
	}
	return &gatewayError, nil
}

func parseNullableMap(value sql.NullString) (map[string]any, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(value.String), &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func calculateLatency(startedAt time.Time, finishedAt *time.Time, usage *UsageMetrics) *int {
	if usage != nil && usage.LatencyMS != nil {
		value := *usage.LatencyMS
		return &value
	}
	if finishedAt == nil {
		return nil
	}
	value := int(finishedAt.Sub(startedAt).Milliseconds())
	return &value
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func cloneStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return map[string]string{}
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func modelConfigHash(config ModelConfig) (string, error) {
	normalized := normalizeModelConfigForHash(config)
	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func normalizeModelConfigForHash(config ModelConfig) ModelConfig {
	normalized := ModelConfig{
		OfflineFailureThreshold: config.OfflineFailureThreshold,
		Retry: RetryConfig{
			MaxAttempts:   cloneIntPointer(config.Retry.MaxAttempts),
			BaseBackoffMS: cloneIntPointer(config.Retry.BaseBackoffMS),
		},
		APIs: make([]APIConfig, 0, len(config.APIs)),
	}
	if config.GlobalMaxConcurrent != nil {
		value := *config.GlobalMaxConcurrent
		normalized.GlobalMaxConcurrent = &value
	}

	for _, api := range config.APIs {
		normalized.APIs = append(normalized.APIs, APIConfig{
			APIID:           api.APIID,
			Provider:        api.Provider,
			APIMode:         api.APIMode,
			Model:           api.Model,
			BaseURL:         api.BaseURL,
			APIKey:          api.APIKey,
			MaxConcurrent:   api.MaxConcurrent,
			Enabled:         api.Enabled,
			Headers:         cloneStringMap(api.Headers),
			RequestDefaults: cloneMap(api.RequestDefaults),
			Pricing:         copyPricing(api.Pricing),
		})
	}
	sort.Slice(normalized.APIs, func(i, j int) bool {
		left := normalized.APIs[i]
		right := normalized.APIs[j]
		if left.APIID != right.APIID {
			return left.APIID < right.APIID
		}
		if left.Provider != right.Provider {
			return left.Provider < right.Provider
		}
		if left.APIMode != right.APIMode {
			return left.APIMode < right.APIMode
		}
		if left.Model != right.Model {
			return left.Model < right.Model
		}
		return left.BaseURL < right.BaseURL
	})
	return normalized
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func copyPricing(value *PricingConfig) *PricingConfig {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func newUUID() string {
	value := time.Now().UnixNano()
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		value&0xffffffff,
		(value>>32)&0xffff,
		(value>>48)&0xffff,
		(value>>16)&0xffff,
		value&0xffffffffffff,
	)
}
