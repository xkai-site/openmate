package main

import (
	"os"

	"vos/internal/vos/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
