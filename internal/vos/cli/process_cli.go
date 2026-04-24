package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"vos/internal/vos/domain"
	"vos/internal/vos/service"
	"vos/internal/vos/store"
)

func runProcess(svc *service.Service, stateFile, sessionDBFile string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || isHelpToken(args[0]) {
		printProcessUsage(stderr)
		if len(args) > 0 {
			return 0
		}
		return 2
	}

	switch args[0] {
	case "list":
		return runProcessList(svc, args[1:], stdout, stderr)
	case "compact":
		sessionStore, err := store.NewSQLiteSessionStore(sessionDBFile)
		if err != nil {
			return printError(err, stderr)
		}
		defer sessionStore.Close()
		svc := service.NewWithSessionStore(store.NewJSONStateStore(stateFile), sessionStore)
		return runProcessCompact(svc, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown process command: %s\n", args[0])
		printProcessUsage(stderr)
		return 2
	}
}

func runProcessList(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vos process list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	nodeID := fs.String("node-id", "", "Node ID")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  vos process list --node-id ID")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if code := parseFlagSet(fs, args); code >= 0 {
		return code
	}

	node, err := svc.GetNode(*nodeID)
	if err != nil {
		return printError(err, stderr)
	}
	return dumpJSON(node.Process, stdout, stderr)
}

func runProcessCompact(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vos process compact", flag.ContinueOnError)
	fs.SetOutput(stderr)
	nodeID := fs.String("node-id", "", "Node ID to compact")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  vos process compact --node-id ID")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if code := parseFlagSet(fs, args); code >= 0 {
		return code
	}

	// Get node with current processes
	node, err := svc.GetNode(*nodeID)
	if err != nil {
		return printError(err, stderr)
	}

	// Get context snapshot to find uncompacted sessions per process
	snapshot, err := svc.GetContextSnapshot(*nodeID)
	if err != nil {
		return printError(err, stderr)
	}

	// Build compact request: node_id + list of processes with their session info
	type processCompactInput struct {
		Process              domain.ProcessItem `json:"process"`
		UncompactedSessionIDs []string           `json:"uncompacted_session_ids"`
	}
	input := struct {
		NodeID    string               `json:"node_id"`
		Processes []processCompactInput `json:"processes"`
		Context   *domain.ContextSnapshot `json:"context,omitempty"`
	}{
		NodeID:  node.ID,
		Context: snapshot,
	}

	for _, proc := range node.Process {
		if proc.SessionRange == nil || proc.SessionRange.StartSessionID == "" {
			continue
		}
		// Determine which sessions in the range aren't compacted yet
		uncompacted := findUncompactedSessions(proc, snapshot)
		if len(uncompacted) == 0 {
			continue
		}
		input.Processes = append(input.Processes, processCompactInput{
			Process:               proc,
			UncompactedSessionIDs: uncompacted,
		})
	}

	if len(input.Processes) == 0 {
		return dumpJSON(map[string]any{
			"status":  "skipped",
			"message": "no processes with uncompacted sessions",
		}, stdout, stderr)
	}

	// Call compact agent via service runner
	runner := service.NewCommandCompactRunner()
	result, err := svc.CompactProcesses(runner, input.NodeID, input.Processes, input.Context)
	if err != nil {
		return printError(err, stderr)
	}

	// Update node's processes with compacted results
	for _, cp := range result.Compacted {
		for i := range node.Process {
			if node.Process[i].Name == cp.Name {
				node.Process[i].Memory = cp.Memory
				node.Process[i].CompactedSessionIDs = cp.CompactedSessionIDs
				break
			}
		}
	}

	if _, err := svc.UpdateNode(service.UpdateNodeInput{
		NodeID:  node.ID,
		ExpectedVersion: &node.Version,
		Process: node.Process,
	}); err != nil {
		return printError(err, stderr)
	}

	return dumpJSON(result, stdout, stderr)
}

// findUncompactedSessions returns session IDs in the process's session_range
// that have not yet been compacted.
func findUncompactedSessions(proc domain.ProcessItem, snapshot *domain.ContextSnapshot) []string {
	compacted := make(map[string]bool)
	for _, sid := range proc.CompactedSessionIDs {
		compacted[sid] = true
	}

	var uncompacted []string
	started := false
	for _, sh := range snapshot.SessionHistory {
		if sh.Session.ID == proc.SessionRange.StartSessionID {
			started = true
		}
		if !started {
			continue
		}
		if !compacted[sh.Session.ID] {
			uncompacted = append(uncompacted, sh.Session.ID)
		}
		if proc.SessionRange.EndSessionID != "" && sh.Session.ID == proc.SessionRange.EndSessionID {
			break
		}
	}
	return uncompacted
}

func printProcessUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage:")
	fmt.Fprintln(writer, "  vos process <list|compact> [flags]")
}

func init() {
	// Ensure JSON encoding of ProcessItem works with compacted_session_ids
	_ = json.Marshal
}
