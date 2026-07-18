package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/mickamy/standin/internal/exit"
)

// Config holds the options for a single generation run.
type Config struct {
	Source      string
	Destination string
	Package     string
	Excludes    []string
}

func Run(args []string, stdout, stderr io.Writer) int {
	cfg, err := parse(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exit.OK
		}

		return exit.Usage
	}

	_ = cfg

	fmt.Fprintln(stderr, "standin: generation is not yet implemented")

	return exit.NotImplemented
}

func parse(args []string, stderr io.Writer) (Config, error) {
	fs := flag.NewFlagSet("standin", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		PrintUsage(stderr)
	}

	var cfg Config

	var exclude string

	fs.StringVar(&cfg.Source, "source", "", "source package to scan (relative path or import path)")
	fs.StringVar(&cfg.Destination, "destination", "", "output directory for the generated file")
	fs.StringVar(&cfg.Package, "package", "", "generated package name (defaults to the destination directory name)")
	fs.StringVar(&exclude, "exclude", "", "comma-separated type names to exclude (e.g., -exclude Foo,Bar)")

	if err := fs.Parse(args); err != nil {
		return Config{}, fmt.Errorf("parse flags: %w", err)
	}

	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "standin: unexpected argument %q\n", fs.Arg(0))
		fs.Usage()

		return Config{}, errors.New("unexpected argument")
	}

	if cfg.Source == "" || cfg.Destination == "" {
		fmt.Fprintln(stderr, "standin: -source and -destination are required")
		fs.Usage()

		return Config{}, errors.New("missing required flags")
	}

	cfg.Excludes = splitExcludes(exclude)

	return cfg, nil
}

func splitExcludes(s string) []string {
	if s == "" {
		return nil
	}

	var names []string

	for name := range strings.SplitSeq(s, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			names = append(names, name)
		}
	}

	return names
}

func PrintUsage(w io.Writer) {
	fmt.Fprintln(w, "standin — generate plain fixture functions from Go structs")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "USAGE:")
	fmt.Fprintln(w, "  standin -source <pkg> -destination <dir> [flags]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "FLAGS:")
	fmt.Fprintln(w, "  -source <pkg>        source package to scan (relative path or import path)")
	fmt.Fprintln(w, "  -destination <dir>   output directory for the generated file")
	fmt.Fprintln(w, "  -package <name>      generated package name (defaults to the destination directory name)")
	fmt.Fprintln(w, "  -exclude <names>     comma-separated type names to exclude (e.g., -exclude Foo,Bar)")
	fmt.Fprintln(w, "  --version, -v        print standin version")
	fmt.Fprintln(w, "  --help, -h           show this help")
}
