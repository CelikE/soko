package main

import (
	"os"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/config"
)

func TestRunAliasArguments(t *testing.T) {
	tests := []struct {
		name    string
		command string
		extra   []string
		want    string
	}{
		{"plain", "alias set captured status", nil, "status"},
		{"double quotes", `alias set captured "status --tag work"`, nil, "status --tag work"},
		{"single quotes", `alias set captured 'exec -- echo "hello world"'`, nil, `exec -- echo "hello world"`},
		{"escaped spaces", `alias set captured status\ --tag\ work`, nil, "status --tag work"},
		{"escaped quotes", `alias set captured "exec -- echo \"hello world\""`, nil, `exec -- echo "hello world"`},
		{"Unicode", `alias set captured 'exec -- echo päth'`, nil, "exec -- echo päth"},
		{"literal expansions", `alias set captured 'exec -- echo $HOME *.go $(whoami)'`, nil, "exec -- echo $HOME *.go $(whoami)"},
		{"appended argument", "alias set captured", []string{`exec -- echo "hello world"`}, `exec -- echo "hello world"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setAliasConfig(t, "capture", tt.command)
			if err := runWithArgs(t, append([]string{"capture"}, tt.extra...)...); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Aliases["captured"]; got != tt.want {
				t.Errorf("captured argument = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunMalformedAlias(t *testing.T) {
	for _, command := range []string{`alias set captured "unterminated`, `alias set captured 'unterminated`, "alias set captured \\", "", "''"} {
		t.Run(command, func(t *testing.T) {
			setAliasConfig(t, "broken", command)
			err := runWithArgs(t, "broken")
			if err == nil || !strings.Contains(err.Error(), `expanding alias "broken"`) {
				t.Fatalf("error = %v, want named alias expansion failure", err)
			}
			cfg, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := cfg.Aliases["captured"]; ok {
				t.Fatal("malformed alias executed a partial command")
			}
		})
	}
}

func TestRunBuiltinBeforeAlias(t *testing.T) {
	setAliasConfig(t, "version", `"unterminated`)
	if err := runWithArgs(t, "version"); err != nil {
		t.Fatalf("built-in command shadowed by alias: %v", err)
	}
}

func setAliasConfig(t *testing.T, name, command string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := config.Save(&config.Config{Aliases: map[string]string{name: command}}); err != nil {
		t.Fatal(err)
	}
}

func runWithArgs(t *testing.T, args ...string) error {
	t.Helper()
	original := os.Args
	t.Cleanup(func() { os.Args = original })
	os.Args = append([]string{"soko"}, args...)
	return run()
}
