package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderPrList(t *testing.T) {
	prs := []prItem{
		{Number: 7, Title: "fix login", Branch: "fix/login", Author: "alice"},
		{Number: 12, Title: "add cache", Branch: "feat/cache", Author: "bob"},
	}
	var buf bytes.Buffer
	renderPrList(&buf, "api", prs)
	out := buf.String()

	for _, want := range []string{"api", "#7", "fix login", "fix/login", "alice", "#12", "bob"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderPrSummary(t *testing.T) {
	results := []prSummaryResult{
		{name: "api", count: 3},
		{name: "web", count: 0},
		{name: "gone", err: "path not found"},
	}
	var buf bytes.Buffer
	renderPrSummary(&buf, results)
	out := buf.String()

	for _, want := range []string{"api", "web", "gone", "path not found", "3 open PRs", "1 repo with PRs"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q:\n%s", want, out)
		}
	}
}
