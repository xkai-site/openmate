package schedule

import (
	"errors"
	"flag"
	"fmt"
	"io"
)

func Run(args []string, stdout, stderr io.Writer) int {
	root := flag.NewFlagSet("openmate-schedule", flag.ContinueOnError)
	root.SetOutput(stderr)
	root.Usage = func() {
		fmt.Fprintln(stdout, "usage: openmate-schedule <command> [flags]")
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "OpenMate schedule control-plane CLI.")
		fmt.Fprintln(stdout, "Module boundaries are frozen at CLI + JSON contracts.")
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Commands:")
		fmt.Fprintln(stdout, "  plan   Print current schedule module contract summary")
	}

	if err := root.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			root.Usage()
			return 0
		}
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
		return runPlan(stdout)
	case "help":
		root.Usage()
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", rest[0])
		root.Usage()
		return 2
	}
}

func runPlan(stdout io.Writer) int {
	fmt.Fprintln(stdout, "{")
	fmt.Fprintln(stdout, `  "module": "schedule",`)
	fmt.Fprintln(stdout, `  "runtime": "go",`)
	fmt.Fprintln(stdout, `  "entrypoint": "cmd/openmate-schedule",`)
	fmt.Fprintln(stdout, `  "internal_package": "internal/schedule",`)
	fmt.Fprintln(stdout, `  "module_boundary": "cli+json",`)
	fmt.Fprintln(stdout, `  "status": "scaffolded"`)
	fmt.Fprintln(stdout, "}")
	return 0
}
