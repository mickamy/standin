package cli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mickamy/standin/internal/exit"
	"github.com/mickamy/standin/internal/gen"
	"github.com/mickamy/standin/internal/infer"
	"github.com/mickamy/standin/internal/parse"
)

// Config holds the options for a single generation run.
type Config struct {
	Source      string
	Destination string
	Package     string
	Excludes    []string
}

func Run(args []string, _, stderr io.Writer) int {
	cfg, err := parseFlags(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exit.OK
		}

		return exit.Usage
	}

	return generate(cfg, stderr)
}

func generate(cfg Config, stderr io.Writer) int {
	pkg, warnings, err := parse.Load(cfg.Source)
	if err != nil {
		fmt.Fprintf(stderr, "standin: %v\n", err)

		return exit.Error
	}

	for _, w := range warnings {
		fmt.Fprintf(stderr, "standin: warning: %s\n", w)
	}

	absDest, err := filepath.Abs(cfg.Destination)
	if err != nil {
		fmt.Fprintf(stderr, "standin: resolve destination: %v\n", err)

		return exit.Error
	}

	// Generating into the source package would make the file import its own
	// package and break compilation.
	if pkg.Dir != "" && absDest == pkg.Dir {
		fmt.Fprintln(stderr, "standin: -destination must be a different package from -source")

		return exit.Usage
	}

	pkgName := cfg.Package
	if pkgName == "" {
		pkgName = filepath.Base(filepath.Clean(cfg.Destination))
	}

	if !token.IsIdentifier(pkgName) {
		fmt.Fprintf(stderr, "standin: invalid package name %q; use -package to override\n", pkgName)

		return exit.Usage
	}

	structs := slices.DeleteFunc(slices.Clone(pkg.Structs), func(s parse.Struct) bool {
		return slices.Contains(cfg.Excludes, s.Name)
	})

	out, err := gen.File(gen.Params{
		PackageName: pkgName,
		SourceName:  pkg.Name,
		SourcePath:  pkg.Path,
		Fixtures:    infer.Fixtures(structs, pkg.Path, pkg.Name),
	})
	if err != nil {
		fmt.Fprintf(stderr, "standin: %v\n", err)

		return exit.Error
	}

	//nolint:gosec // generated source directories are meant to be world-readable
	if err := os.MkdirAll(cfg.Destination, 0o755); err != nil {
		fmt.Fprintf(stderr, "standin: create destination: %v\n", err)

		return exit.Error
	}

	path := filepath.Join(cfg.Destination, gen.FileName)

	//nolint:gosec // generated source is meant to be world-readable
	if err := os.WriteFile(path, out, 0o644); err != nil {
		fmt.Fprintf(stderr, "standin: write %s: %v\n", path, err)

		return exit.Error
	}

	if needsTidy(out, pkg.GoMod) {
		fmt.Fprintf(stderr, "standin: note: generated code imports %s; run 'go mod tidy' to add it\n", gen.GofakeitImport)
	}

	return exit.OK
}

// needsTidy reports whether the generated code imports gofakeit while the
// module's go.mod does not mention it yet.
func needsTidy(out []byte, gomod string) bool {
	if gomod == "" || !bytes.Contains(out, []byte(gen.GofakeitImport)) {
		return false
	}

	//nolint:gosec // the go.mod path comes from the build system metadata
	content, err := os.ReadFile(gomod)
	if err != nil {
		return false
	}

	return !bytes.Contains(content, []byte(gen.GofakeitImport))
}

func parseFlags(args []string, stderr io.Writer) (Config, error) {
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
