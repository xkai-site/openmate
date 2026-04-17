package main

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	root := flag.NewFlagSet("openmate-vos-web", flag.ContinueOnError)
	root.SetOutput(os.Stderr)
	addr := root.String("addr", "127.0.0.1:8081", "Frontend listen address")
	webDir := root.String("web-dir", filepath.FromSlash("frontend/vos"), "Frontend static files directory")
	root.Usage = func() {
		fmt.Fprintln(os.Stdout, "usage: openmate-vos-web [--addr ADDR] [--web-dir PATH]")
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, "Starts VOS frontend static server (HTML/CSS/JS).")
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, "Example:")
		fmt.Fprintln(os.Stdout, "  openmate-vos-web --addr 127.0.0.1:8081")
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

	resolvedWebDir := filepath.Clean(strings.TrimSpace(*webDir))
	if resolvedWebDir == "" {
		fmt.Fprintln(os.Stderr, "web-dir must not be empty")
		return 2
	}
	info, err := os.Stat(resolvedWebDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "web-dir not found: %s\n", resolvedWebDir)
		return 2
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "web-dir is not a directory: %s\n", resolvedWebDir)
		return 2
	}

	mux := http.NewServeMux()
	fileServer := http.FileServer(http.Dir(resolvedWebDir))
	mux.Handle("/", fileServer)

	fmt.Fprintf(os.Stdout, "VOS frontend listening on http://%s\n", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
