package main

import (
	"os"

	"github.com/mickamy/standin/internal/cli"
)

// version is set by goreleaser via ldflags.
var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], version, os.Stdout, os.Stderr))
}
