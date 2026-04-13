package schedule

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
		fmt.Fprintln(stdout, "  plan   Build one topic dispatch plan from a topic snapshot JSON file")
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
		return runPlan(rest[1:], stdout, stderr)
	case "help":
		root.Usage()
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", rest[0])
		root.Usage()
		return 2
	}
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

func dumpJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
