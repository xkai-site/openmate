package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vos/internal/openmate/observability"
)

func Run(args []string, stdout, stderr io.Writer) int {
	root := flag.NewFlagSet("openmate-schedule", flag.ContinueOnError)
	root.SetOutput(stderr)
	dbFile := root.String("db-file", defaultUnifiedDBFile(), "Unified SQLite database path for schedule runtime and VOS sessions")
	runtimeDBFile := root.String("runtime-db-file", "", "Schedule runtime SQLite database path (overrides --db-file)")
	workdir := root.String("workdir", ".", "Working directory for worker command execution")
	vosMode := root.String("vos-mode", "direct", "VOS gateway mode: direct or shell")
	vosCommandRaw := root.String("vos-command", defaultVOSCommand(), "Command used to invoke VOS CLI")
	vosStateFile := root.String("vos-state-file", filepath.FromSlash(".openmate/runtime/vos_state.json"), "VOS state file path passed to vos command")
	vosSessionDBFile := root.String("vos-session-db-file", "", "VOS session database path passed to vos command (overrides --db-file)")
	workerCommandRaw := root.String("worker-command", defaultWorkerCommand(), "Command used to invoke agent worker CLI")
	defaultTimeoutMS := root.Int("default-timeout-ms", 120000, "Default worker timeout in milliseconds")
	agingSeconds := root.Int("aging-seconds", 600, "Topic aging promotion threshold in seconds")
	logLevel := root.String("log-level", "info", "Log level: debug|info|warn|error")
	logFormat := root.String("log-format", "json", "Log format: json|text")
	root.Usage = func() {
		fmt.Fprintln(stdout, "usage: openmate-schedule <command> [flags]")
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "OpenMate schedule control-plane CLI.")
		fmt.Fprintln(stdout, "Default path uses direct Go VOS gateway; Python worker stays on CLI + JSON boundary.")
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Commands:")
		fmt.Fprintln(stdout, "  plan   Build one topic dispatch plan from a topic snapshot JSON file")
		fmt.Fprintln(stdout, "  enqueue  Insert or update one node in schedule runtime queue")
		fmt.Fprintln(stdout, "  tick   Run one scheduler tick")
		fmt.Fprintln(stdout, "  run    Run scheduler ticks in a loop")
		fmt.Fprintln(stdout, "  state  Print current scheduler runtime state")
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Global flags:")
		root.PrintDefaults()
	}

	if err := root.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			root.Usage()
			return 0
		}
		fmt.Fprintln(stderr, err)
		return 2
	}
	logger, err := observability.NewLogger(observability.Config{
		Level:  *logLevel,
		Format: *logFormat,
		Writer: stderr,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	logger = logger.With(slog.String(observability.FieldComponent, "schedule"))

	setFlags := map[string]bool{}
	root.Visit(func(flagValue *flag.Flag) {
		setFlags[flagValue.Name] = true
	})
	resolvedRuntimeDBFile, resolvedVOSSessionDBFile, err := resolveRuntimeDBFiles(
		*dbFile,
		*runtimeDBFile,
		*vosSessionDBFile,
		setFlags,
	)
	if err != nil {
		logger.Error("resolve runtime db files failed", slog.Any("error", err))
		fmt.Fprintln(stderr, err)
		return 2
	}

	rest := root.Args()
	if len(rest) == 0 {
		root.Usage()
		return 2
	}

	switch rest[0] {
	case "plan":
		return runPlan(rest[1:], stdout, stderr)
	case "enqueue":
		return runEnqueue(rest[1:], stdout, stderr, runtimeCommandConfig{
			RuntimeDBFile:    resolvedRuntimeDBFile,
			Workdir:          *workdir,
			VOSMode:          *vosMode,
			VOSCommand:       splitCommand(*vosCommandRaw),
			VOSStateFile:     *vosStateFile,
			VOSSessionDBFile: resolvedVOSSessionDBFile,
			WorkerCommand:    splitCommand(*workerCommandRaw),
			DefaultTimeoutMS: *defaultTimeoutMS,
			AgingThreshold:   time.Duration(*agingSeconds) * time.Second,
			Logger:           logger,
		})
	case "tick":
		return runTick(rest[1:], stdout, stderr, runtimeCommandConfig{
			RuntimeDBFile:    resolvedRuntimeDBFile,
			Workdir:          *workdir,
			VOSMode:          *vosMode,
			VOSCommand:       splitCommand(*vosCommandRaw),
			VOSStateFile:     *vosStateFile,
			VOSSessionDBFile: resolvedVOSSessionDBFile,
			WorkerCommand:    splitCommand(*workerCommandRaw),
			DefaultTimeoutMS: *defaultTimeoutMS,
			AgingThreshold:   time.Duration(*agingSeconds) * time.Second,
			Logger:           logger,
		})
	case "run":
		return runLoop(rest[1:], stdout, stderr, runtimeCommandConfig{
			RuntimeDBFile:    resolvedRuntimeDBFile,
			Workdir:          *workdir,
			VOSMode:          *vosMode,
			VOSCommand:       splitCommand(*vosCommandRaw),
			VOSStateFile:     *vosStateFile,
			VOSSessionDBFile: resolvedVOSSessionDBFile,
			WorkerCommand:    splitCommand(*workerCommandRaw),
			DefaultTimeoutMS: *defaultTimeoutMS,
			AgingThreshold:   time.Duration(*agingSeconds) * time.Second,
			Logger:           logger,
		})
	case "state":
		return runState(rest[1:], stdout, stderr, runtimeCommandConfig{
			RuntimeDBFile:    resolvedRuntimeDBFile,
			Workdir:          *workdir,
			VOSMode:          *vosMode,
			VOSCommand:       splitCommand(*vosCommandRaw),
			VOSStateFile:     *vosStateFile,
			VOSSessionDBFile: resolvedVOSSessionDBFile,
			WorkerCommand:    splitCommand(*workerCommandRaw),
			DefaultTimeoutMS: *defaultTimeoutMS,
			AgingThreshold:   time.Duration(*agingSeconds) * time.Second,
			Logger:           logger,
		})
	case "help":
		root.Usage()
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", rest[0])
		root.Usage()
		return 2
	}
}

type runtimeCommandConfig struct {
	RuntimeDBFile    string
	Workdir          string
	VOSMode          string
	VOSCommand       []string
	VOSStateFile     string
	VOSSessionDBFile string
	WorkerCommand    []string
	DefaultTimeoutMS int
	AgingThreshold   time.Duration
	Logger           *slog.Logger
}

func runPlan(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("openmate-schedule plan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	inputFile := fs.String("input-file", "", "Path to TopicSnapshot JSON file")
	availableSlots := fs.Int("available-slots", 1, "Available agent slots for this topic")
	fs.Usage = func() {
		fmt.Fprintln(stdout, "usage: openmate-schedule plan --input-file PATH [--available-slots N]")
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Input JSON schema: TopicSnapshot.")
		fmt.Fprintln(stdout, "Output JSON schema: DispatchPlan.")
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.Usage()
			return 0
		}
		fmt.Fprintln(stderr, err)
		return 2
	}
	if *inputFile == "" {
		fmt.Fprintln(stderr, "input-file is required")
		return 2
	}

	payload, err := os.ReadFile(filepath.Clean(*inputFile))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	topic, err := ParseTopicSnapshotJSON(payload)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	plan, err := planTopicDispatch(topic, *availableSlots)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := dumpJSON(stdout, plan); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func runEnqueue(args []string, stdout, stderr io.Writer, config runtimeCommandConfig) int {
	fs := flag.NewFlagSet("openmate-schedule enqueue", flag.ContinueOnError)
	fs.SetOutput(stderr)
	requestFile := fs.String("request-file", "", "Path to EnqueueRequest JSON file")
	requestJSON := fs.String("request-json", "", "Inline EnqueueRequest JSON")
	fs.Usage = func() {
		fmt.Fprintln(stdout, "usage: openmate-schedule enqueue (--request-file PATH | --request-json JSON)")
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Input JSON schema: EnqueueRequest.")
		fmt.Fprintln(stdout, "Output JSON schema: EnqueueResult.")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.Usage()
			return 0
		}
		fmt.Fprintln(stderr, err)
		return 2
	}
	if (*requestFile == "" && *requestJSON == "") || (*requestFile != "" && *requestJSON != "") {
		fmt.Fprintln(stderr, "enqueue requires exactly one of --request-file or --request-json")
		return 2
	}
	raw, err := loadInputPayload(*requestFile, *requestJSON)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	var request EnqueueRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		fmt.Fprintln(stderr, "invalid enqueue request json:", err)
		return 2
	}

	engine, closer, err := openEngine(config)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer closer()

	ctx := observability.WithLogger(context.Background(), config.Logger)
	result, err := engine.Enqueue(ctx, request)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := dumpJSON(stdout, result); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func runTick(args []string, stdout, stderr io.Writer, config runtimeCommandConfig) int {
	fs := flag.NewFlagSet("openmate-schedule tick", flag.ContinueOnError)
	fs.SetOutput(stderr)
	maxDispatch := fs.Int("max-dispatch", 1, "Max dispatch count in this tick")
	fs.Usage = func() {
		fmt.Fprintln(stdout, "usage: openmate-schedule tick [--max-dispatch N]")
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Output JSON schema: TickResult.")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.Usage()
			return 0
		}
		fmt.Fprintln(stderr, err)
		return 2
	}
	engine, closer, err := openEngine(config)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer closer()

	ctx := observability.WithLogger(context.Background(), config.Logger)
	result, err := engine.Tick(ctx, *maxDispatch)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := dumpJSON(stdout, result); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func runLoop(args []string, stdout, stderr io.Writer, config runtimeCommandConfig) int {
	fs := flag.NewFlagSet("openmate-schedule run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	intervalMS := fs.Int("interval-ms", 1000, "Tick interval in milliseconds")
	maxDispatch := fs.Int("max-dispatch-per-tick", 1, "Max dispatch count per tick")
	maxTicks := fs.Int("max-ticks", 0, "Stop after N ticks (0 means no hard limit)")
	untilIdle := fs.Bool("until-idle", true, "Stop when one tick dispatches nothing")
	fs.Usage = func() {
		fmt.Fprintln(stdout, "usage: openmate-schedule run [--interval-ms N] [--max-dispatch-per-tick N] [--max-ticks N] [--until-idle]")
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Output JSON schema: []TickResult.")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.Usage()
			return 0
		}
		fmt.Fprintln(stderr, err)
		return 2
	}
	engine, closer, err := openEngine(config)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer closer()
	ctx := observability.WithLogger(context.Background(), config.Logger)

	results := []TickResult{}
	ticker := time.NewTicker(time.Duration(*intervalMS) * time.Millisecond)
	defer ticker.Stop()

	for tick := 0; ; tick++ {
		result, err := engine.Tick(ctx, *maxDispatch)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		results = append(results, result)
		if *untilIdle && len(result.Dispatched) == 0 {
			break
		}
		if *maxTicks > 0 && tick+1 >= *maxTicks {
			break
		}
		<-ticker.C
	}
	if err := dumpJSON(stdout, results); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func runState(args []string, stdout, stderr io.Writer, config runtimeCommandConfig) int {
	fs := flag.NewFlagSet("openmate-schedule state", flag.ContinueOnError)
	fs.SetOutput(stderr)
	topicID := fs.String("topic-id", "", "Optional topic ID")
	fs.Usage = func() {
		fmt.Fprintln(stdout, "usage: openmate-schedule state [--topic-id ID]")
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Output JSON schema: list of TopicSnapshot.")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.Usage()
			return 0
		}
		fmt.Fprintln(stderr, err)
		return 2
	}
	store, err := OpenRuntimeStore(config.RuntimeDBFile, nil)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() {
		_ = store.Close()
	}()

	snapshots := []TopicSnapshot{}
	if *topicID != "" {
		snapshot, err := store.BuildTopicSnapshot(*topicID)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		snapshots = append(snapshots, snapshot)
	} else {
		topics, err := store.ListTopics()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		for _, topic := range topics {
			snapshot, err := store.BuildTopicSnapshot(topic.TopicID)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			snapshots = append(snapshots, snapshot)
		}
	}
	if err := dumpJSON(stdout, snapshots); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func dumpJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func loadInputPayload(requestFile, requestJSON string) ([]byte, error) {
	if requestFile != "" {
		return os.ReadFile(filepath.Clean(requestFile))
	}
	return []byte(requestJSON), nil
}

func resolveRuntimeDBFiles(
	dbFile string,
	runtimeDBFile string,
	vosSessionDBFile string,
	setFlags map[string]bool,
) (string, string, error) {
	unified := strings.TrimSpace(dbFile)
	if unified == "" {
		return "", "", ValidationError{Message: "db-file must not be empty"}
	}

	runtimePath := strings.TrimSpace(runtimeDBFile)
	if !setFlags["runtime-db-file"] {
		runtimePath = unified
	}
	if runtimePath == "" {
		return "", "", ValidationError{Message: "runtime-db-file must not be empty"}
	}

	vosSessionPath := strings.TrimSpace(vosSessionDBFile)
	if !setFlags["vos-session-db-file"] {
		vosSessionPath = unified
	}
	if vosSessionPath == "" {
		return "", "", ValidationError{Message: "vos-session-db-file must not be empty"}
	}

	return runtimePath, vosSessionPath, nil
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

func openEngine(config runtimeCommandConfig) (*Engine, func(), error) {
	store, err := OpenRuntimeStore(config.RuntimeDBFile, nil)
	if err != nil {
		return nil, nil, err
	}

	cleanups := []func(){
		func() { _ = store.Close() },
	}
	cleanup := func() {
		for index := len(cleanups) - 1; index >= 0; index-- {
			cleanups[index]()
		}
	}

	var vosGateway VOSGateway
	switch strings.ToLower(strings.TrimSpace(config.VOSMode)) {
	case "", "direct":
		directGateway, closeDirect, directErr := OpenDirectVOSGateway(config.VOSStateFile, config.VOSSessionDBFile)
		if directErr != nil {
			cleanup()
			return nil, nil, directErr
		}
		if closeDirect != nil {
			cleanups = append(cleanups, closeDirect)
		}
		vosGateway = directGateway
	case "shell":
		shellGateway, shellErr := NewShellVOSGateway(ShellGatewayConfig{
			Workdir:          config.Workdir,
			VOSCommand:       config.VOSCommand,
			VOSStateFile:     config.VOSStateFile,
			VOSSessionDBFile: config.VOSSessionDBFile,
		})
		if shellErr != nil {
			cleanup()
			return nil, nil, shellErr
		}
		vosGateway = shellGateway
	default:
		cleanup()
		return nil, nil, ValidationError{Message: "vos-mode must be one of: direct, shell"}
	}

	workerGateway, err := NewShellWorkerGateway(ShellGatewayConfig{
		Workdir:       config.Workdir,
		WorkerCommand: config.WorkerCommand,
	})
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	engine, err := NewEngine(
		store,
		vosGateway,
		workerGateway,
		EngineConfig{
			MaxDispatchPerTick: 1,
			DefaultTimeoutMS:   config.DefaultTimeoutMS,
			AgingThreshold:     config.AgingThreshold,
			Logger:             config.Logger,
		},
		nil,
	)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return engine, cleanup, nil
}

func defaultVOSCommand() string {
	defaultBinary := filepath.FromSlash(".openmate/bin/vos.exe")
	if _, err := os.Stat(defaultBinary); err == nil {
		return defaultBinary
	}
	return "go run ./cmd/vos"
}

func defaultUnifiedDBFile() string {
	return filepath.FromSlash(".openmate/runtime/openmate.db")
}

func defaultWorkerCommand() string {
	venvPython := filepath.FromSlash(".venv/Scripts/python.exe")
	if _, err := os.Stat(venvPython); err == nil {
		return venvPython + " -m openmate_agent.cli worker run"
	}
	return "python -m openmate_agent.cli worker run"
}
