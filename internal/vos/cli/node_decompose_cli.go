package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"vos/internal/vos/domain"
	"vos/internal/vos/service"
	"vos/internal/vos/store"
)

func runNodeDecompose(_ *service.Service, stateFile string, sessionDBFile string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vos node decompose", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		nodeID       = fs.String("node-id", "", "Node ID to decompose")
		hint         = fs.String("hint", "", "Optional decompose hint")
		maxItems     = fs.Int("max-items", service.DefaultNodeDecomposeMaxItems, "Maximum task count from decompose agent")
		agentCommand = fs.String("agent-command", service.DefaultDecomposeAgentCommand(), "Command used to invoke decompose agent CLI")
	)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  vos node decompose --node-id ID [flags]")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if code := parseFlagSet(fs, args); code >= 0 {
		return code
	}
	if *maxItems <= 0 {
		return printError(domain.ValidationError{Message: "max-items must be > 0"}, stderr)
	}

	sessionStore, err := store.NewSQLiteSessionStore(sessionDBFile)
	if err != nil {
		return printError(err, stderr)
	}
	defer sessionStore.Close()

	decomposeService := service.NewWithSessionStore(store.NewJSONStateStore(stateFile), sessionStore)
	runner := service.NewCommandDecomposeRunner(strings.TrimSpace(*agentCommand))
	result, err := decomposeService.DecomposeNode(context.Background(), service.NodeDecomposeInput{
		NodeID:   strings.TrimSpace(*nodeID),
		Hint:     strings.TrimSpace(*hint),
		MaxItems: *maxItems,
	}, runner)
	if err != nil {
		return printError(err, stderr)
	}
	return dumpJSON(result, stdout, stderr)
}
