package runtime

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"vos/internal/openmate/observability"
	openmatepaths "vos/internal/openmate/paths"
	"vos/internal/poolgateway"
	"vos/internal/schedule"
	vosservice "vos/internal/vos/service"
	vosstore "vos/internal/vos/store"
)

type Config struct {
	StateFile       string
	SessionDBFile   string
	ScheduleDBFile  string
	WorkspaceRoot   string
	ModelConfigFile string
	WorkerCommand   []string
	DefaultTimeout  int
	AgingThreshold  time.Duration
	Logger          *slog.Logger
}

type Runtime struct {
	Service        *vosservice.Service
	ScheduleStore  *schedule.RuntimeStore
	ScheduleEngine *schedule.Engine
	PoolStore      *poolgateway.Store
	PoolGateway    *poolgateway.Gateway

	sessionStore vosstore.SessionStore
	logger       *slog.Logger
}

func Open(config Config) (*Runtime, error) {
	logger := observability.NormalizeLogger(config.Logger).With(
		slog.String(observability.FieldComponent, "runtime"),
		slog.String(observability.FieldOperation, "runtime.open"),
	)
	stateFile := strings.TrimSpace(config.StateFile)
	sessionDBFile := strings.TrimSpace(config.SessionDBFile)
	scheduleDBFile := strings.TrimSpace(config.ScheduleDBFile)
	workspaceRoot := strings.TrimSpace(config.WorkspaceRoot)
	if stateFile == "" {
		logger.Error("state file is required")
		return nil, schedule.ValidationError{Message: "state file is required"}
	}
	if sessionDBFile == "" {
		logger.Error("session db file is required")
		return nil, schedule.ValidationError{Message: "session db file is required"}
	}
	if scheduleDBFile == "" {
		scheduleDBFile = sessionDBFile
	}
	if workspaceRoot == "" {
		workspaceRoot = "."
	}
	modelConfigFile := strings.TrimSpace(config.ModelConfigFile)
	if modelConfigFile == "" {
		modelConfigFile = filepath.Join(filepath.Clean(workspaceRoot), "model.json")
	}

	sessionStore, err := vosstore.NewSQLiteSessionStore(sessionDBFile)
	if err != nil {
		logger.Error("open vos session store failed", slog.Any("error", err))
		return nil, err
	}
	service := vosservice.NewWithSessionStore(
		vosstore.NewJSONStateStore(stateFile),
		sessionStore,
	)

	scheduleStore, err := schedule.OpenRuntimeStore(scheduleDBFile, nil)
	if err != nil {
		logger.Error("open schedule runtime store failed", slog.Any("error", err))
		_ = sessionStore.Close()
		return nil, err
	}
	poolStore, err := poolgateway.NewStore(sessionDBFile)
	if err != nil {
		logger.Error("open pool store failed", slog.Any("error", err))
		_ = scheduleStore.Close()
		_ = sessionStore.Close()
		return nil, err
	}

	vosGateway, err := schedule.NewDirectVOSGateway(service)
	if err != nil {
		logger.Error("create direct vos gateway failed", slog.Any("error", err))
		_ = poolStore.Close()
		_ = scheduleStore.Close()
		_ = sessionStore.Close()
		return nil, err
	}
	workerGateway, err := schedule.NewShellWorkerGateway(schedule.ShellGatewayConfig{
		Workdir:       workspaceRoot,
		WorkerCommand: normalizeWorkerCommand(config.WorkerCommand, workspaceRoot),
	})
	if err != nil {
		logger.Error("create shell worker gateway failed", slog.Any("error", err))
		_ = poolStore.Close()
		_ = scheduleStore.Close()
		_ = sessionStore.Close()
		return nil, err
	}

	engine, err := schedule.NewEngine(
		scheduleStore,
		vosGateway,
		workerGateway,
		schedule.EngineConfig{
			MaxDispatchPerTick: 1,
			DefaultTimeoutMS:   config.DefaultTimeout,
			AgingThreshold:     config.AgingThreshold,
			Logger:             logger.With(slog.String(observability.FieldComponent, "schedule")),
		},
		nil,
	)
	if err != nil {
		logger.Error("create schedule engine failed", slog.Any("error", err))
		_ = poolStore.Close()
		_ = scheduleStore.Close()
		_ = sessionStore.Close()
		return nil, err
	}

	poolGateway := poolgateway.NewGateway(poolStore, modelConfigFile)
	poolGateway.SetLogger(logger.With(slog.String(observability.FieldComponent, "pool")))
	if _, err := poolGateway.Sync(context.Background()); err != nil {
		logger.Error("initialize pool from model config failed", slog.Any("error", err))
		_ = poolStore.Close()
		_ = scheduleStore.Close()
		_ = sessionStore.Close()
		return nil, err
	}

	logger.Info(
		"runtime opened",
		slog.String("state_file", stateFile),
		slog.String("session_db_file", sessionDBFile),
		slog.String("schedule_db_file", scheduleDBFile),
	)
	return &Runtime{
		Service:        service,
		ScheduleStore:  scheduleStore,
		ScheduleEngine: engine,
		PoolStore:      poolStore,
		PoolGateway:    poolGateway,
		sessionStore:   sessionStore,
		logger:         logger,
	}, nil
}

func (runtime *Runtime) Close() error {
	if runtime == nil {
		return nil
	}
	logger := observability.NormalizeLogger(runtime.logger).With(
		slog.String(observability.FieldComponent, "runtime"),
		slog.String(observability.FieldOperation, "runtime.close"),
	)
	var firstErr error
	if runtime.ScheduleStore != nil {
		if err := runtime.ScheduleStore.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if runtime.PoolStore != nil {
		if err := runtime.PoolStore.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if runtime.sessionStore != nil {
		if err := runtime.sessionStore.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		logger.Error("runtime close failed", slog.Any("error", firstErr))
		return firstErr
	}
	logger.Info("runtime closed")
	return firstErr
}

func normalizeWorkerCommand(command []string, workspaceRoot string) []string {
	result := make([]string, 0, len(command))
	for _, value := range command {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	if len(result) > 0 {
		return result
	}
	return splitCommand(openmatepaths.DefaultWorkerCommand(workspaceRoot))
}

func splitCommand(raw string) []string {
	fields := strings.Fields(strings.TrimSpace(raw))
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		if field == "" {
			continue
		}
		result = append(result, field)
	}
	return result
}
