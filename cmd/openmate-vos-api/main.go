package main

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"vos/internal/vos/httpapi"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	root := flag.NewFlagSet("openmate-vos-api", flag.ContinueOnError)
	root.SetOutput(os.Stderr)
	addr := root.String("addr", "127.0.0.1:8080", "HTTP listen address")
	dbFile := root.String("db-file", filepath.FromSlash(".openmate/runtime/openmate.db"), "Unified SQLite database path for VOS sessions")
	stateFile := root.String("state-file", filepath.FromSlash(".openmate/runtime/vos_state.json"), "JSON state file path")
	sessionDBFile := root.String("session-db-file", "", "SQLite session database path (overrides --db-file)")
	root.Usage = func() {
		fmt.Fprintln(os.Stdout, "usage: openmate-vos-api [--addr ADDR] [--db-file PATH] [--state-file PATH] [--session-db-file PATH]")
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

	server, err := httpapi.NewServer(httpapi.Config{
		StateFile:     *stateFile,
		SessionDBFile: resolvedSessionDBFile,
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
