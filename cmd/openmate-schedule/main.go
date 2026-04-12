package main

import (
	"os"

	"vos/internal/schedule"
)

func main() {
	os.Exit(schedule.Run(os.Args[1:], os.Stdout, os.Stderr))
}
