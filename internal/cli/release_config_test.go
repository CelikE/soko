package cli_test

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestReleaseAuditConfiguration(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name            string
				Run             string
				Uses            string
				If              string
				ContinueOnError bool `yaml:"continue-on-error"`
				Env             map[string]string
			}
		}
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	syncIndex, publishIndex := -1, -1
	for i, step := range workflow.Jobs["release"].Steps {
		if strings.Contains(step.Run, "merge-upstream") {
			syncIndex = i
			if step.ContinueOnError || step.If != "" || !strings.Contains(step.Run, "--method POST repos/CelikE/winget-pkgs/merge-upstream -f branch=master") || step.Env["GH_TOKEN"] != "${{ secrets.WINGET_TOKEN }}" {
				t.Fatal("manifest synchronization must run unconditionally and fail closed before publication")
			}
		}
		if strings.HasPrefix(step.Uses, "goreleaser/goreleaser-action@") {
			publishIndex = i
		}
	}
	if syncIndex < 0 || publishIndex <= syncIndex {
		t.Fatal("Windows manifest fork must synchronize before GoReleaser publishes")
	}
	data, err = os.ReadFile("../../.goreleaser.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var release struct {
		Casks []struct {
			Manpages []string
		} `yaml:"homebrew_casks"`
	}
	if err := yaml.Unmarshal(data, &release); err != nil {
		t.Fatal(err)
	}
	if len(release.Casks) != 1 || len(release.Casks[0].Manpages) != 1 || release.Casks[0].Manpages[0] != "man/soko.1" {
		t.Fatal("generated Homebrew cask must install the archived manpage")
	}
}
