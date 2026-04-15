package cli

import (
	"flag"
	"fmt"
	"io"

	"vos/internal/vos/service"
)

func runContext(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || isHelpToken(args[0]) {
		printContextUsage(stderr)
		if len(args) > 0 {
			return 0
		}
		return 2
	}

	switch args[0] {
	case "snapshot":
		return runContextSnapshot(svc, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown context command: %s\n", args[0])
		printContextUsage(stderr)
		return 2
	}
}

func runContextSnapshot(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vos context snapshot", flag.ContinueOnError)
	fs.SetOutput(stderr)
	nodeID := fs.String("node-id", "", "Node ID")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  vos context snapshot --node-id ID")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if code := parseFlagSet(fs, args); code >= 0 {
		return code
	}

	snapshot, err := svc.GetContextSnapshot(*nodeID)
	if err != nil {
		return printError(err, stderr)
	}
	return dumpJSON(snapshot, stdout, stderr)
}

func printContextUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage:")
	fmt.Fprintln(writer, "  vos context <snapshot> [flags]")
}
