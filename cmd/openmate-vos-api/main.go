package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"vos/internal/openmate/observability"
	openmatepaths "vos/internal/openmate/paths"
	"vos/internal/vos/httpapi"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	root := flag.NewFlagSet("openmate-vos-api", flag.ContinueOnError)
	root.SetOutput(os.Stderr)
	addr := root.String("addr", "127.0.0.1:8080", "HTTP listen address")
	dbFile := root.String("db-file", openmatepaths.DefaultUnifiedDBFile(), "Unified SQLite database path for VOS sessions")
	stateFile := root.String("state-file", openmatepaths.DefaultVOSStateFile(), "JSON state file path")
	sessionDBFile := root.String("session-db-file", "", "SQLite session database path (overrides --db-file)")
	scheduleDBFile := root.String("schedule-db-file", "", "Schedule runtime database path (defaults to --db-file)")
	scheduleMode := root.String("schedule-mode", "inproc", "Schedule execution mode: inproc or shell")
	scheduleCommandRaw := root.String("schedule-command", "", "Schedule command in shell mode")
	workerCommandRaw := root.String("worker-command", "", "Worker command used by inproc schedule engine")
	defaultTimeoutMS := root.Int("default-timeout-ms", 120000, "Default worker timeout in milliseconds for inproc scheduler")
	agingSeconds := root.Int("aging-seconds", 600, "Topic aging threshold in seconds for inproc scheduler")
	logLevel := root.String("log-level", "info", "Log level: debug|info|warn|error")
	logFormat := root.String("log-format", "json", "Log format: json|text")
	root.Usage = func() {
		fmt.Fprintln(os.Stdout, "usage: openmate-vos-api [--addr ADDR] [--db-file PATH] [--state-file PATH] [--session-db-file PATH] [--schedule-mode inproc|shell]")
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, "Starts VOS JSON HTTP API server.")
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, "Example:")
		fmt.Fprintln(os.Stdout, "  openmate-vos-api --addr 127.0.0.1:8080")
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, "Flags:")
		root.PrintDefaults()
	}

	if err := root.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	logger, err := observability.NewLogger(observability.Config{
		Level:  *logLevel,
		Format: *logFormat,
		Writer: os.Stderr,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	logger = logger.With(slog.String(observability.FieldComponent, "vos-api"))

	setFlags := map[string]bool{}
	root.Visit(func(flagValue *flag.Flag) {
		setFlags[flagValue.Name] = true
	})
	resolvedSessionDBFile := strings.TrimSpace(*sessionDBFile)
	if !setFlags["session-db-file"] {
		resolvedSessionDBFile = strings.TrimSpace(*dbFile)
	}
	if strings.TrimSpace(*stateFile) == "" {
		fmt.Fprintln(os.Stderr, "state-file must not be empty")
		return 2
	}
	if resolvedSessionDBFile == "" {
		fmt.Fprintln(os.Stderr, "session-db-file must not be empty")
		return 2
	}
	resolvedScheduleDBFile := strings.TrimSpace(*scheduleDBFile)
	if resolvedScheduleDBFile == "" {
		resolvedScheduleDBFile = strings.TrimSpace(*dbFile)
	}
	if resolvedScheduleDBFile == "" {
		fmt.Fprintln(os.Stderr, "schedule-db-file must not be empty")
		return 2
	}

	server, err := httpapi.NewServer(httpapi.Config{
		StateFile:        *stateFile,
		SessionDBFile:    resolvedSessionDBFile,
		ScheduleDB:       resolvedScheduleDBFile,
		ScheduleMode:     *scheduleMode,
		ScheduleCmd:      splitCommand(*scheduleCommandRaw),
		WorkerCommand:    splitCommand(*workerCommandRaw),
		DefaultTimeoutMS: *defaultTimeoutMS,
		AgingSeconds:     *agingSeconds,
		Logger:           logger,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() {
		_ = server.Close()
	}()

	fmt.Fprintf(os.Stdout, "VOS API server listening on http://%s\n", *addr)
	if err := http.ListenAndServe(*addr, server.Handler()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
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
