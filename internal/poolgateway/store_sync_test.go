package poolgateway

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestStoreSyncFromModelConfigSkipsWriteWhenHashUnchanged(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "model.json")
	writeModelConfig(t, configPath, 3)

	config, err := LoadModelConfig(configPath)
	if err != nil {
		t.Fatalf("load model config: %v", err)
	}

	store, err := NewStore(filepath.Join(tempDir, "pool_state.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.SyncFromModelConfig(ctx, config); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	if _, err := store.db.ExecContext(
		ctx,
		`UPDATE apis
		   SET failure_count = ?, last_error = ?, status = ?
		 WHERE api_id = ?`,
		2,
		"keep-last-error",
		string(ApiStatusOffline),
		"api-1",
	); err != nil {
		t.Fatalf("seed api runtime state: %v", err)
	}

	if err := store.SyncFromModelConfig(ctx, config); err != nil {
		t.Fatalf("second sync with same config: %v", err)
	}

	var failureCount int
	var lastError sql.NullString
	var status string
	if err := store.db.QueryRowContext(
		ctx,
		`SELECT failure_count, last_error, status FROM apis WHERE api_id = ?`,
		"api-1",
	).Scan(&failureCount, &lastError, &status); err != nil {
		t.Fatalf("query api runtime state: %v", err)
	}
	if failureCount != 2 {
		t.Fatalf("failure_count changed unexpectedly: %d", failureCount)
	}
	if !lastError.Valid || lastError.String != "keep-last-error" {
		t.Fatalf("last_error changed unexpectedly: %+v", lastError)
	}
	if status != string(ApiStatusOffline) {
		t.Fatalf("status changed unexpectedly: %s", status)
	}

	hash, err := readMetaValue(ctx, store, metaKeyModelConfigHash)
	if err != nil {
		t.Fatalf("read model hash: %v", err)
	}
	if hash == nil || *hash == "" {
		t.Fatalf("expected model config hash to be persisted")
	}
}

func TestStoreSyncFromModelConfigAppliesWhenHashChanged(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "model.json")
	writeModelConfig(t, configPath, 3)

	config, err := LoadModelConfig(configPath)
	if err != nil {
		t.Fatalf("load model config: %v", err)
	}

	store, err := NewStore(filepath.Join(tempDir, "pool_state.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.SyncFromModelConfig(ctx, config); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	firstHash, err := readMetaValue(ctx, store, metaKeyModelConfigHash)
	if err != nil {
		t.Fatalf("read first model hash: %v", err)
	}
	if firstHash == nil || *firstHash == "" {
		t.Fatalf("expected first model hash")
	}

	configChanged := config
	configChanged.APIs = append([]APIConfig(nil), config.APIs...)
	configChanged.APIs[0] = config.APIs[0]
	configChanged.APIs[0].MaxConcurrent = 2
	if err := store.SyncFromModelConfig(ctx, configChanged); err != nil {
		t.Fatalf("sync changed config: %v", err)
	}

	secondHash, err := readMetaValue(ctx, store, metaKeyModelConfigHash)
	if err != nil {
		t.Fatalf("read second model hash: %v", err)
	}
	if secondHash == nil || *secondHash == "" {
		t.Fatalf("expected second model hash")
	}
	if *firstHash == *secondHash {
		t.Fatalf("expected different hashes after config change")
	}

	var maxConcurrent int
	if err := store.db.QueryRowContext(
		ctx,
		`SELECT max_concurrent FROM apis WHERE api_id = ?`,
		"api-1",
	).Scan(&maxConcurrent); err != nil {
		t.Fatalf("query max_concurrent: %v", err)
	}
	if maxConcurrent != 2 {
		t.Fatalf("max_concurrent not updated, got %d", maxConcurrent)
	}
}

func readMetaValue(ctx context.Context, store *Store, key string) (*string, error) {
	row := store.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key)
	var value string
	if err := row.Scan(&value); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &value, nil
}
