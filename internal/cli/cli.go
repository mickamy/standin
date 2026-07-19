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

	"golang.org/x/mod/modfile"

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
	ShowVersion bool
}

func Run(args []string, version string, stdout, stderr io.Writer) int {
	cfg, err := parseFlags(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exit.OK
		}

		return exit.Usage
	}

	if cfg.ShowVersion {
		fmt.Fprintf(stdout, "standin %s\n", version)

		return exit.OK
	}

	return generate(cfg, stderr)
}

func generate(cfg Config, stderr io.Writer) int {
	// Validate what we can before the expensive package load.
	pkgName := cfg.Package
	if pkgName == "" {
		pkgName = filepath.Base(filepath.Clean(cfg.Destination))
	}

	if !token.IsIdentifier(pkgName) {
		fmt.Fprintf(stderr, "standin: invalid package name %q; use -package to override\n", pkgName)

		return exit.Usage
	}

	absDest, err := filepath.Abs(cfg.Destination)
	if err != nil {
		fmt.Fprintf(stderr, "standin: resolve destination: %v\n", err)

		return exit.Error
	}

	pkg, warnings, err := parse.Load(cfg.Source)
	if err != nil {
		fmt.Fprintf(stderr, "standin: %v\n", err)

		return exit.Error
	}

	for _, w := range warnings {
		fmt.Fprintf(stderr, "standin: warning: %s\n", w)
	}

	// Generating into the source package would make the file import its own
	// package and break compilation.
	if pkg.Dir != "" && absDest == pkg.Dir {
		fmt.Fprintln(stderr, "standin: -destination must be a different package from -source")

		return exit.Usage
	}

	names := make(map[string]bool, len(pkg.Structs))
	for _, s := range pkg.Structs {
		names[s.Name] = true
	}

	for _, ex := range cfg.Excludes {
		if !names[ex] {
			fmt.Fprintf(stderr, "standin: warning: -exclude %s matches no struct in %s\n", ex, pkg.Path)
		}
	}

	structs := slices.DeleteFunc(slices.Clone(pkg.Structs), func(s parse.Struct) bool {
		return slices.Contains(cfg.Excludes, s.Name)
	})

	if len(structs) == 0 {
		fmt.Fprintf(stderr, "standin: no fixture targets found in %s\n", pkg.Path)

		return exit.Error
	}

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

	if needsTidy(out, absDest) {
		fmt.Fprintf(stderr, "standin: note: generated code imports %s; run 'go mod tidy' to add it\n", gen.GofakeitImport)
	}

	return exit.OK
}

// needsTidy reports whether the generated code imports gofakeit while the
// destination module's go.mod does not require it yet. It is best-effort:
// an unknown module layout just suppresses the note.
func needsTidy(out []byte, destDir string) bool {
	if !bytes.Contains(out, []byte(gen.GofakeitImport)) {
		return false
	}

	gomod := findGoMod(destDir)
	if gomod == "" {
		return false
	}

	//nolint:gosec // the go.mod path is discovered next to the destination
	data, err := os.ReadFile(gomod)
	if err != nil {
		return false
	}

	f, err := modfile.Parse(gomod, data, nil)
	if err != nil {
		return false
	}

	for _, r := range f.Require {
		if r.Mod.Path == gen.GofakeitImport {
			return false
		}
	}

	return true
}

// findGoMod walks up from dir to locate the enclosing go.mod file.
func findGoMod(dir string) string {
	for {
		path := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(path); err == nil {
			return path
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}

		dir = parent
	}
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
	fs.BoolVar(&cfg.ShowVersion, "version", false, "print standin version")
	fs.BoolVar(&cfg.ShowVersion, "v", false, "print standin version (shorthand)")

	if err := fs.Parse(args); err != nil {
		return Config{}, fmt.Errorf("parse flags: %w", err)
	}

	if cfg.ShowVersion {
		return cfg, nil
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
