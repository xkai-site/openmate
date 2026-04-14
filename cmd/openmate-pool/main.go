package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"openmatepool/internal/poolgateway"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	root := flag.NewFlagSet("pool", flag.ContinueOnError)
	root.SetOutput(os.Stdout)
	dbFile := root.String("db-file", ".pool_state.db", "SQLite state database path")
	modelConfig := root.String("model-config", "model.json", "Model config JSON path")
	root.Usage = func() {
		fmt.Fprintln(os.Stdout, "usage: pool [--db-file DB_FILE] [--model-config MODEL_CONFIG] {invoke,cap,records,usage,sync} ...")
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, "OpenMate LLM gateway CLI. All commands print JSON to stdout.")
	}
	if err := root.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			root.Usage()
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

	store, err := poolgateway.NewStore(*dbFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer store.Close()

	gateway := poolgateway.NewGateway(store, *modelConfig)

	switch rest[0] {
	case "invoke":
		return runInvoke(rest[1:], gateway)
	case "cap":
		return runCap(rest[1:], gateway)
	case "records":
		return runRecords(rest[1:], gateway)
	case "usage":
		return runUsage(rest[1:], gateway)
	case "sync":
		return runSync(rest[1:], gateway)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", rest[0])
		root.Usage()
		return 2
	}
}

func runInvoke(args []string, gateway *poolgateway.Gateway) int {
	cmd := flag.NewFlagSet("invoke", flag.ContinueOnError)
	cmd.SetOutput(os.Stdout)
	requestJSON := cmd.String("request-json", "", "Inline InvokeRequest JSON")
	requestFile := cmd.String("request-file", "", "Path to InvokeRequest JSON file")
	cmd.Usage = func() {
		fmt.Fprintln(os.Stdout, "usage: pool invoke (--request-json JSON | --request-file PATH)")
	}
	if err := cmd.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			cmd.Usage()
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

	response, err := gateway.Invoke(context.Background(), request)
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

func runCap(args []string, gateway *poolgateway.Gateway) int {
	cmd := flag.NewFlagSet("cap", flag.ContinueOnError)
	cmd.SetOutput(os.Stdout)
	if err := cmd.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	capacity, err := gateway.Capacity(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	printJSON(capacity)
	return 0
}

func runRecords(args []string, gateway *poolgateway.Gateway) int {
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
	records, err := gateway.Records(context.Background(), nodeFilter, limitFilter)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	printJSON(records)
	return 0
}

func runSync(args []string, gateway *poolgateway.Gateway) int {
	cmd := flag.NewFlagSet("sync", flag.ContinueOnError)
	cmd.SetOutput(os.Stdout)
	if err := cmd.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	result, err := gateway.Sync(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	printJSON(result)
	return 0
}

func runUsage(args []string, gateway *poolgateway.Gateway) int {
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
	summary, err := gateway.Usage(context.Background(), nodeFilter, limitFilter)
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

func intPtr(value int) *int {
	return &value
}

func parseInt(value string) *int {
	if value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return nil
	}
	return intPtr(parsed)
}
