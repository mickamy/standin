package cli_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/mickamy/standin/internal/cli"
	"github.com/mickamy/standin/internal/exit"
)

func TestRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want int
	}{
		{
			name: "no args",
			args: nil,
			want: exit.Usage,
		},
		{
			name: "missing source",
			args: []string{"-destination", "./fixture"},
			want: exit.Usage,
		},
		{
			name: "missing destination",
			args: []string{"-source", "./model"},
			want: exit.Usage,
		},
		{
			name: "unknown flag",
			args: []string{"-source", "./model", "-destination", "./fixture", "-bogus"},
			want: exit.Usage,
		},
		{
			name: "unexpected argument",
			args: []string{"-source", "./model", "-destination", "./fixture", "extra"},
			want: exit.Usage,
		},
		{
			name: "help",
			args: []string{"-h"},
			want: exit.OK,
		},
		{
			name: "valid",
			args: []string{"-source", "./model", "-destination", "./fixture"},
			want: exit.NotImplemented,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer

			got := cli.Run(tt.args, &stdout, &stderr)
			if got != tt.want {
				t.Errorf("Run() = %d, want %d\nstderr: %s", got, tt.want, stderr.String())
			}
		})
	}
}

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want cli.Config
	}{
		{
			name: "required only",
			args: []string{"-source", "./model", "-destination", "./fixture"},
			want: cli.Config{Source: "./model", Destination: "./fixture"},
		},
		{
			name: "all flags",
			args: []string{"-source", "./model", "-destination", "./fixture", "-package", "myfixture", "-exclude", "Foo,Bar"},
			want: cli.Config{
				Source:      "./model",
				Destination: "./fixture",
				Package:     "myfixture",
				Excludes:    []string{"Foo", "Bar"},
			},
		},
		{
			name: "exclude with spaces and empties",
			args: []string{"-source", "./model", "-destination", "./fixture", "-exclude", " Foo, ,Bar,"},
			want: cli.Config{Source: "./model", Destination: "./fixture", Excludes: []string{"Foo", "Bar"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer

			got, err := cli.Parse(tt.args, &stderr)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parse() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
