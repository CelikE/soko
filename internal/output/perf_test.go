package output

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Microsecond, "0ms"},
		{88 * time.Millisecond, "88ms"},
		{412 * time.Millisecond, "412ms"},
		{time.Second, "1.0s"},
		{1310 * time.Millisecond, "1.3s"},
		{9040 * time.Millisecond, "9.0s"},
	}
	for _, c := range cases {
		if got := FormatDuration(c.d); got != c.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestRenderTiming(t *testing.T) {
	SetPerf(true)
	t.Cleanup(func() { SetPerf(false) })

	var buf bytes.Buffer
	rows := []TimingRow{
		{Name: "api", Duration: 412 * time.Millisecond},
		{Name: "web", Duration: 88 * time.Millisecond},
		{Name: "monorepo", Duration: 1310 * time.Millisecond},
	}
	RenderTiming(&buf, rows, 1840*time.Millisecond, 8)
	got := buf.String()

	if !strings.Contains(got, "timing — 3 repos · 8 max concurrency") {
		t.Errorf("missing header with repo count + concurrency, got:\n%s", got)
	}
	// Slowest first: monorepo must appear before api, api before web.
	mono := strings.Index(got, "monorepo")
	api := strings.Index(got, "api")
	web := strings.Index(got, "web")
	if mono < 0 || api <= mono || web <= api {
		t.Errorf("rows not slowest-first (mono=%d api=%d web=%d):\n%s", mono, api, web, got)
	}
	// Footer: wall and git (git = 1310+412+88 = 1810ms ≈ 1.8s).
	if !strings.Contains(got, "wall 1.8s") || !strings.Contains(got, "git 1.8s") {
		t.Errorf("missing/incorrect wall·git footer, got:\n%s", got)
	}
}

func TestRenderTimingNoOp(t *testing.T) {
	// Off: nothing rendered even with rows.
	SetPerf(false)
	var buf bytes.Buffer
	RenderTiming(&buf, []TimingRow{{Name: "api", Duration: time.Second}}, time.Second, 8)
	if buf.Len() != 0 {
		t.Errorf("RenderTiming with perf off should render nothing, got %q", buf.String())
	}

	// On but no rows: still nothing.
	SetPerf(true)
	t.Cleanup(func() { SetPerf(false) })
	buf.Reset()
	RenderTiming(&buf, nil, time.Second, 8)
	if buf.Len() != 0 {
		t.Errorf("RenderTiming with empty rows should render nothing, got %q", buf.String())
	}
}

func TestRenderTimingCap(t *testing.T) {
	SetPerf(true)
	t.Cleanup(func() { SetPerf(false) })

	rows := make([]TimingRow, 30)
	for i := range rows {
		// Descending durations so repo-00 is slowest and ordering is stable.
		rows[i] = TimingRow{Name: fmt.Sprintf("repo-%02d", i), Duration: time.Duration(30-i) * time.Second}
	}
	var buf bytes.Buffer
	RenderTiming(&buf, rows, 30*time.Second, 8)
	got := buf.String()

	if !strings.Contains(got, "… 5 more") {
		t.Errorf("expected '… 5 more' for 30 rows over the cap of 25, got:\n%s", got)
	}
	// The 26th-slowest onward are hidden.
	if strings.Contains(got, "repo-29") {
		t.Errorf("hidden row repo-29 should not appear, got:\n%s", got)
	}
	if !strings.Contains(got, "repo-24") {
		t.Errorf("last visible row repo-24 should appear, got:\n%s", got)
	}
}

func TestBuildTiming(t *testing.T) {
	rows := []TimingRow{
		{Name: "api", Duration: 412 * time.Millisecond},
		{Name: "monorepo", Duration: 1310 * time.Millisecond},
	}
	env := BuildTiming(rows, 1840*time.Millisecond, 8)

	if env.WallMS != 1840 {
		t.Errorf("WallMS = %d, want 1840", env.WallMS)
	}
	if env.GitMS != 1722 { // 412 + 1310
		t.Errorf("GitMS = %d, want 1722", env.GitMS)
	}
	if env.Repos != 2 {
		t.Errorf("Repos = %d, want 2", env.Repos)
	}
	if env.Concurrency != 8 {
		t.Errorf("Concurrency = %d, want 8", env.Concurrency)
	}
	if len(env.Slowest) != 2 || env.Slowest[0].Name != "monorepo" || env.Slowest[0].DurationMS != 1310 {
		t.Errorf("Slowest = %+v, want monorepo (1310ms) first", env.Slowest)
	}
	if env.Slowest[1].Name != "api" {
		t.Errorf("Slowest[1] = %q, want api", env.Slowest[1].Name)
	}
}
