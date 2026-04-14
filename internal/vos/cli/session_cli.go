package cli

import (
	"flag"
	"fmt"
	"io"

	"vos/internal/vos/domain"
	"vos/internal/vos/service"
)

func runSession(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || isHelpToken(args[0]) {
		printSessionUsage(stderr)
		if len(args) > 0 {
			return 0
		}
		return 2
	}

	switch args[0] {
	case "create":
		return runSessionCreate(svc, args[1:], stdout, stderr)
	case "get":
		return runSessionGet(svc, args[1:], stdout, stderr)
	case "append-event":
		return runSessionAppendEvent(svc, args[1:], stdout, stderr)
	case "events":
		return runSessionEvents(svc, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown session command: %s\n", args[0])
		printSessionUsage(stderr)
		return 2
	}
}

func runSessionCreate(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vos session create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		nodeID    = fs.String("node-id", "", "Node ID")
		sessionID = fs.String("session-id", "", "Optional session ID")
		statusRaw = fs.String("status", string(domain.SessionStatusOpen), "Initial session status")
	)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  vos session create --node-id ID [flags]")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if code := parseFlagSet(fs, args); code >= 0 {
		return code
	}

	status, err := domain.ParseSessionStatus(*statusRaw)
	if err != nil {
		return printError(err, stderr)
	}
	session, err := svc.CreateSession(service.CreateSessionInput{
		NodeID:    *nodeID,
		SessionID: *sessionID,
		Status:    status,
	})
	if err != nil {
		return printError(err, stderr)
	}
	return dumpJSON(session, stdout, stderr)
}

func runSessionGet(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vos session get", flag.ContinueOnError)
	fs.SetOutput(stderr)
	sessionID := fs.String("session-id", "", "Session ID")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  vos session get --session-id ID")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if code := parseFlagSet(fs, args); code >= 0 {
		return code
	}

	session, err := svc.GetSession(*sessionID)
	if err != nil {
		return printError(err, stderr)
	}
	return dumpJSON(session, stdout, stderr)
}

func runSessionAppendEvent(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vos session append-event", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		sessionID   = fs.String("session-id", "", "Session ID")
		kindRaw     = fs.String("kind", "", "Session event kind")
		roleRaw     = fs.String("role", "", "Optional session event role")
		callID      = fs.String("call-id", "", "Optional tool call correlation ID")
		payloadJSON = fs.String("payload-json", "{}", "Session event payload JSON object")
	)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  vos session append-event --session-id ID --kind KIND [flags]")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if code := parseFlagSet(fs, args); code >= 0 {
		return code
	}

	kind, err := domain.ParseSessionEventKind(*kindRaw)
	if err != nil {
		return printError(err, stderr)
	}
	var role *domain.SessionRole
	if *roleRaw != "" {
		parsedRole, err := domain.ParseSessionRole(*roleRaw)
		if err != nil {
			return printError(err, stderr)
		}
		role = &parsedRole
	}
	payload, err := parseJSONObject(*payloadJSON, "payload-json")
	if err != nil {
		return printError(err, stderr)
	}

	input := service.AppendSessionEventInput{
		SessionID:   *sessionID,
		Kind:        kind,
		Role:        role,
		PayloadJSON: payload,
	}
	if *callID != "" {
		input.CallID = stringPtr(*callID)
	}

	event, err := svc.AppendSessionEvent(input)
	if err != nil {
		return printError(err, stderr)
	}
	return dumpJSON(event, stdout, stderr)
}

func runSessionEvents(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vos session events", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		sessionID = fs.String("session-id", "", "Session ID")
		afterSeq  = fs.Int("after-seq", 0, "Return events with seq greater than this value")
		limit     = fs.Int("limit", 100, "Maximum event count to return")
	)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  vos session events --session-id ID [flags]")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if code := parseFlagSet(fs, args); code >= 0 {
		return code
	}

	events, err := svc.ListSessionEvents(*sessionID, *afterSeq, *limit)
	if err != nil {
		return printError(err, stderr)
	}
	return dumpJSON(events, stdout, stderr)
}

func printSessionUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage:")
	fmt.Fprintln(writer, "  vos session <create|get|append-event|events> [flags]")
}
