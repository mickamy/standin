package cli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
	absDest, err := filepath.Abs(cfg.Destination)
	if err != nil {
		fmt.Fprintf(stderr, "standin: resolve destination: %v\n", err)

		return exit.Error
	}

	// Validate what we can before the expensive package load.
	pkgName := packageName(cfg.Package, absDest)
	if !token.IsIdentifier(pkgName) {
		fmt.Fprintf(stderr, "standin: invalid package name %q; use -package to override\n", pkgName)

		return exit.Usage
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
	// package and break compilation. Compare with symlinks resolved so an
	// aliased path (e.g., /tmp vs /private/tmp on macOS) cannot bypass the
	// check.
	if pkg.Dir != "" && resolvePath(absDest) == resolvePath(pkg.Dir) {
		fmt.Fprintln(stderr, "standin: -destination must be a different package from -source")

		return exit.Usage
	}

	names := make(map[string]bool, len(pkg.Structs))
	for _, s := range pkg.Structs {
		names[s.Name] = true
	}

	excluded := make(map[string]bool, len(cfg.Excludes))

	for _, ex := range cfg.Excludes {
		if !names[ex] {
			fmt.Fprintf(stderr, "standin: warning: -exclude %s matches no struct in %s\n", ex, pkg.Path)
		}

		excluded[ex] = true
	}

	structs := slices.DeleteFunc(slices.Clone(pkg.Structs), func(s parse.Struct) bool {
		return excluded[s.Name]
	})

	if len(structs) == 0 {
		fmt.Fprintf(stderr, "standin: no fixture targets found in %s\n", pkg.Path)

		return exit.Error
	}

	fixtures, imports := infer.Fixtures(structs, pkg.Path, pkg.Name)

	out, err := gen.File(gen.Params{
		PackageName: pkgName,
		SourceName:  pkg.Name,
		SourcePath:  pkg.Path,
		Imports:     imports,
		Fixtures:    fixtures,
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

	if missing := missingRequires(out, imports, absDest); len(missing) > 0 {
		fmt.Fprintf(stderr, "standin: note: generated code imports %s; run 'go mod tidy'\n", strings.Join(missing, ", "))
	}

	return exit.OK
}

// packageName resolves the package name of the generated file. destDir must
// be absolute, so that a relative -destination such as "." still yields a
// directory name.
func packageName(override, destDir string) string {
	if override != "" {
		return override
	}

	if name := declaredPackage(destDir); name != "" {
		return name
	}

	return filepath.Base(destDir)
}

// declaredPackage returns the package the Go files in dir already declare;
// every file in a directory has to agree on it, so the generated file has no
// choice either. The generated file itself is skipped so a name written by a
// previous run cannot pin the next one, and test files are skipped because
// they may sit in an external _test package. An unreadable directory yields an
// empty name.
func declaredPackage(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	fset := token.NewFileSet()

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == gen.FileName {
			continue
		}

		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.PackageClauseOnly)
		if err != nil {
			continue
		}

		return f.Name.Name
	}

	return ""
}

// missingRequires returns the third-party imports of the generated code that
// the destination module's go.mod does not require yet, sorted. It is
// best-effort: an unknown module layout just suppresses the note.
func missingRequires(out []byte, imports []string, destDir string) []string {
	gomod := findGoMod(destDir)
	if gomod == "" {
		return nil
	}

	//nolint:gosec // the go.mod path is discovered next to the destination
	data, err := os.ReadFile(gomod)
	if err != nil {
		return nil
	}

	f, err := modfile.Parse(gomod, data, nil)
	if err != nil {
		return nil
	}

	required := make(map[string]bool, len(f.Require))
	for _, r := range f.Require {
		required[r.Mod.Path] = true
	}

	candidates := append(slices.Clone(imports), gen.GofakeitImport)
	slices.Sort(candidates)

	var missing []string

	for _, path := range candidates {
		// gofakeit is a candidate whether or not the output uses it, so match
		// against the import line the generator actually wrote.
		if required[path] || !bytes.Contains(out, []byte(strconv.Quote(path))) {
			continue
		}

		missing = append(missing, path)
	}

	return missing
}

// resolvePath resolves symlinks best-effort, falling back to the input when
// the path cannot be resolved (e.g., it does not exist yet).
func resolvePath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}

	return resolved
}

// findGoMod walks up from dir to locate the enclosing go.mod file.
func findGoMod(dir string) string {
	// Normalize so the parent walk reaches the filesystem root even for
	// relative inputs.
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}

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
	fmt.Fprintln(w, "  -package <name>      generated package name (defaults to the package the destination declares)")
	fmt.Fprintln(w, "  -exclude <names>     comma-separated type names to exclude (e.g., -exclude Foo,Bar)")
	fmt.Fprintln(w, "  --version, -v        print standin version")
	fmt.Fprintln(w, "  --help, -h           show this help")
}
