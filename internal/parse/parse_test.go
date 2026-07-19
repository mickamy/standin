package parse_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mickamy/standin/internal/parse"
)

const modelPath = "github.com/mickamy/standin/internal/parse/testdata/model"

type fieldView struct {
	name string
	typ  string
	tag  reflect.StructTag
}

func viewOf(f parse.Field) fieldView {
	return fieldView{
		name: f.Name,
		typ:  f.Type.String(),
		tag:  f.Tag,
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()

	pkg, warnings, err := parse.Load("./testdata/model")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if pkg.Name != "model" {
		t.Errorf("Name = %q, want %q", pkg.Name, "model")
	}

	if pkg.Path != modelPath {
		t.Errorf("Path = %q, want %q", pkg.Path, modelPath)
	}

	wantDir, err := filepath.Abs(filepath.Join("testdata", "model"))
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v", err)
	}

	if pkg.Dir != wantDir {
		t.Errorf("Dir = %q, want %q", pkg.Dir, wantDir)
	}

	wantWarnings := []string{"skipping generic struct Pair"}
	if !reflect.DeepEqual(warnings, wantWarnings) {
		t.Errorf("warnings = %v, want %v", warnings, wantWarnings)
	}

	wantStructs := map[string][]fieldView{
		"Admin": {
			{name: "Profile", typ: modelPath + ".Profile"},
			{name: "Level", typ: "int"},
		},
		"NotIgnored": {
			{name: "A", typ: "int"},
		},
		"Profile": {
			{name: "Bio", typ: "string"},
		},
		"SpacedIgnore": {
			{name: "A", typ: "int"},
		},
		"User": {
			{name: "ID", typ: "int64"},
			{name: "Name", typ: "string", tag: `fake:"{firstname}"`},
			{name: "CreatedAt", typ: "time.Time"},
			{name: "Profile", typ: modelPath + ".Profile"},
			{name: "Note", typ: "*string"},
		},
	}

	wantNames := []string{"Admin", "NotIgnored", "Profile", "SpacedIgnore", "User"}

	gotNames := make([]string, 0, len(pkg.Structs))
	for _, s := range pkg.Structs {
		gotNames = append(gotNames, s.Name)
	}

	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("struct names = %v, want %v", gotNames, wantNames)
	}

	for _, s := range pkg.Structs {
		got := make([]fieldView, 0, len(s.Fields))
		for _, f := range s.Fields {
			got = append(got, viewOf(f))
		}

		if !reflect.DeepEqual(got, wantStructs[s.Name]) {
			t.Errorf("%s fields = %+v, want %+v", s.Name, got, wantStructs[s.Name])
		}
	}
}

func TestLoadErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
	}{
		{
			name:    "nonexistent directory",
			pattern: "./testdata/nonexistent",
		},
		{
			name:    "multiple packages",
			pattern: "../...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := parse.Load(tt.pattern)
			if err == nil {
				t.Error("Load() error = nil, want non-nil")
			}
		})
	}
}
