package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"vos/internal/vos/service"
)

func runMemory(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || isHelpToken(args[0]) {
		printMemoryUsage(stderr)
		if len(args) > 0 {
			return 0
		}
		return 2
	}

	switch args[0] {
	case "proposal":
		return runMemoryProposal(svc, args[1:], stdout, stderr)
	case "apply":
		return runMemoryApply(svc, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown memory command: %s\n", args[0])
		printMemoryUsage(stderr)
		return 2
	}
}

func runMemoryProposal(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || isHelpToken(args[0]) {
		printMemoryProposalUsage(stderr)
		if len(args) > 0 {
			return 0
		}
		return 2
	}
	if args[0] != "list" {
		fmt.Fprintf(stderr, "unknown memory proposal command: %s\n", args[0])
		printMemoryProposalUsage(stderr)
		return 2
	}

	fs := flag.NewFlagSet("vos memory proposal list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	topicID := fs.String("topic-id", "", "Topic ID")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  vos memory proposal list --topic-id ID")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if code := parseFlagSet(fs, args[1:]); code >= 0 {
		return code
	}

	items, err := svc.ListTopicMemoryProposals(*topicID)
	if err != nil {
		return printError(err, stderr)
	}
	return dumpJSON(items, stdout, stderr)
}

func runMemoryApply(svc *service.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vos memory apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	topicID := fs.String("topic-id", "", "Topic ID")
	proposalID := fs.String("proposal-id", "", "Proposal ID")
	decision := fs.String("decision", "", "Decision: confirm or reject")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage:")
		fmt.Fprintln(fs.Output(), "  vos memory apply --topic-id ID --proposal-id ID --decision confirm|reject")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	if code := parseFlagSet(fs, args); code >= 0 {
		return code
	}

	result, err := svc.ApplyTopicMemoryProposal(*topicID, *proposalID, service.MemoryApplyDecision(strings.ToLower(strings.TrimSpace(*decision))))
	if err != nil {
		return printError(err, stderr)
	}
	return dumpJSON(result, stdout, stderr)
}

func printMemoryUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage:")
	fmt.Fprintln(writer, "  vos memory <proposal|apply> [flags]")
}

func printMemoryProposalUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage:")
	fmt.Fprintln(writer, "  vos memory proposal list --topic-id ID")
}
