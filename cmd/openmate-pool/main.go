package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"vos/internal/openmate/observability"
	openmatepaths "vos/internal/openmate/paths"
	"vos/internal/poolgateway"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	root := flag.NewFlagSet("pool", flag.ContinueOnError)
	root.SetOutput(os.Stdout)
	dbFile := root.String("db-file", openmatepaths.DefaultUnifiedDBFile(), "SQLite state database path")
	modelConfig := root.String("model-config", "model.json", "Model config JSON path")
	logLevel := root.String("log-level", "info", "Log level: debug|info|warn|error")
	logFormat := root.String("log-format", "json", "Log format: json|text")
	root.Usage = func() {
		fmt.Fprintln(os.Stdout, "usage: pool [--db-file DB_FILE] [--model-config MODEL_CONFIG] {invoke,cap,records,usage,sync} ...")
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, "OpenMate LLM gateway CLI. All commands print JSON to stdout.")
	}
	if err := root.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	rest := root.Args()
	if len(rest) == 0 {
		root.Usage()
		return 1
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
	logger = logger.With(slog.String(observability.FieldComponent, "pool"))

	store, err := poolgateway.NewStore(*dbFile)
	if err != nil {
		logger.Error("open pool store failed", slog.Any("error", err))
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer store.Close()

	gateway := poolgateway.NewGateway(store, *modelConfig)
	gateway.SetLogger(logger)

	switch rest[0] {
	case "invoke":
		return runInvoke(rest[1:], gateway, logger)
	case "cap":
		return runCap(rest[1:], gateway, logger)
	case "records":
		return runRecords(rest[1:], gateway, logger)
	case "usage":
		return runUsage(rest[1:], gateway, logger)
	case "sync":
		return runSync(rest[1:], gateway, logger)
	default:
		logger.Error("unknown command", slog.String("command", rest[0]))
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", rest[0])
		root.Usage()
		return 2
	}
}

func runInvoke(args []string, gateway *poolgateway.Gateway, logger *slog.Logger) int {
	cmd := flag.NewFlagSet("invoke", flag.ContinueOnError)
	cmd.SetOutput(os.Stdout)
	requestJSON := cmd.String("request-json", "", "Inline InvokeRequest JSON")
	requestFile := cmd.String("request-file", "", "Path to InvokeRequest JSON file")
	cmd.Usage = func() {
		fmt.Fprintln(os.Stdout, "usage: pool invoke (--request-json JSON | --request-file PATH)")
	}
	if err := cmd.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if (*requestJSON == "" && *requestFile == "") || (*requestJSON != "" && *requestFile != "") {
		fmt.Fprintln(os.Stderr, "invoke requires exactly one of --request-json or --request-file")
		return 2
	}

	payload, err := loadRequestPayload(*requestJSON, *requestFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var request poolgateway.InvokeRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	request = normalizeRequest(request)

	ctx := observability.WithLogger(context.Background(), logger)
	response, err := gateway.Invoke(ctx, request)
	if err != nil {
		if invocationErr, ok := err.(*poolgateway.InvocationFailedError); ok {
			printJSON(invocationErr.Response)
			return 2
		}
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	printJSON(response)
	return 0
}

func runCap(args []string, gateway *poolgateway.Gateway, logger *slog.Logger) int {
	cmd := flag.NewFlagSet("cap", flag.ContinueOnError)
	cmd.SetOutput(os.Stdout)
	if err := cmd.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	ctx := observability.WithLogger(context.Background(), logger)
	capacity, err := gateway.Capacity(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	printJSON(capacity)
	return 0
}

func runRecords(args []string, gateway *poolgateway.Gateway, logger *slog.Logger) int {
	cmd := flag.NewFlagSet("records", flag.ContinueOnError)
	cmd.SetOutput(os.Stdout)
	nodeID := cmd.String("node-id", "", "Filter by node ID")
	limit := cmd.Int("limit", 0, "Return only last N records")
	if err := cmd.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var nodeFilter *string
	if *nodeID != "" {
		nodeFilter = nodeID
	}
	var limitFilter *int
	if *limit > 0 {
		limitFilter = limit
	}
	ctx := observability.WithLogger(context.Background(), logger)
	records, err := gateway.Records(ctx, nodeFilter, limitFilter)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	printJSON(records)
	return 0
}

func runSync(args []string, gateway *poolgateway.Gateway, logger *slog.Logger) int {
	cmd := flag.NewFlagSet("sync", flag.ContinueOnError)
	cmd.SetOutput(os.Stdout)
	if err := cmd.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	ctx := observability.WithLogger(context.Background(), logger)
	result, err := gateway.Sync(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	printJSON(result)
	return 0
}

func runUsage(args []string, gateway *poolgateway.Gateway, logger *slog.Logger) int {
	cmd := flag.NewFlagSet("usage", flag.ContinueOnError)
	cmd.SetOutput(os.Stdout)
	nodeID := cmd.String("node-id", "", "Filter by node ID")
	limit := cmd.Int("limit", 0, "Aggregate only the last N records")
	if err := cmd.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var nodeFilter *string
	if *nodeID != "" {
		nodeFilter = nodeID
	}
	var limitFilter *int
	if *limit > 0 {
		limitFilter = limit
	}
	ctx := observability.WithLogger(context.Background(), logger)
	summary, err := gateway.Usage(ctx, nodeFilter, limitFilter)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	printJSON(summary)
	return 0
}

func normalizeRequest(request poolgateway.InvokeRequest) poolgateway.InvokeRequest {
	if request.Request == nil {
		request.Request = poolgateway.OpenAIResponsesRequest{}
	}
	return request
}

func loadRequestPayload(requestJSON string, requestFile string) ([]byte, error) {
	if requestJSON != "" {
		return []byte(requestJSON), nil
	}
	content, err := os.ReadFile(filepath.Clean(requestFile))
	if err != nil {
		return nil, err
	}
	return content, nil
}

func printJSON(value any) {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	fmt.Fprintln(os.Stdout, string(payload))
}
