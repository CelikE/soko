package output

import (
	"bytes"
	"testing"
)

func TestQuietSuppressesChrome(t *testing.T) {
	t.Cleanup(func() { SetQuiet(false) })

	emitters := map[string]func(w *bytes.Buffer){
		"Info":                func(w *bytes.Buffer) { Info(w, "hi") },
		"Warn":                func(w *bytes.Buffer) { Warn(w, "careful") },
		"RenderSummary":       func(w *bytes.Buffer) { RenderSummary(w, 3, 1, 0, 2) },
		"RenderActionSummary": func(w *bytes.Buffer) { RenderActionSummary(w, 3, 2, 1) },
		"RenderFetchSummary":  func(w *bytes.Buffer) { RenderFetchSummary(w, 3, 2, 1) },
		"RenderPullSummary":   func(w *bytes.Buffer) { RenderPullSummary(w, 3, 1, 1, 0, 1) },
		"RenderHealthSummary": func(w *bytes.Buffer) { RenderHealthSummary(w, 3, 1, 1, 1) },
		"RenderGrepSummary":   func(w *bytes.Buffer) { RenderGrepSummary(w, 2, 5, false) },
		"RenderRemotesSummary": func(w *bytes.Buffer) {
			RenderRemotesSummary(w, 3, 2, 1)
		},
	}

	for name, emit := range emitters {
		t.Run(name, func(t *testing.T) {
			SetQuiet(true)
			var quietBuf bytes.Buffer
			emit(&quietBuf)
			if quietBuf.Len() != 0 {
				t.Errorf("%s in quiet mode wrote %q, want nothing", name, quietBuf.String())
			}

			SetQuiet(false)
			var loudBuf bytes.Buffer
			emit(&loudBuf)
			if loudBuf.Len() == 0 {
				t.Errorf("%s with quiet off wrote nothing, want output", name)
			}
		})
	}
}

func TestQuietLeavesPrimaryOutput(t *testing.T) {
	t.Cleanup(func() { SetQuiet(false) })
	SetQuiet(true)

	// Confirm, Fail, and tables are primary output — never gated.
	keep := map[string]func(w *bytes.Buffer){
		"Confirm": func(w *bytes.Buffer) { Confirm(w, "done") },
		"Fail":    func(w *bytes.Buffer) { Fail(w, "boom") },
		"RenderStatusTable": func(w *bytes.Buffer) {
			RenderStatusTable(w, []StatusRow{{Name: "api", Branch: "main", State: StateClean}})
		},
		"RenderActionResults": func(w *bytes.Buffer) {
			RenderActionResults(w, []ActionRow{{Name: "api", Success: true, Message: "ok"}})
		},
	}
	for name, emit := range keep {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			emit(&buf)
			if buf.Len() == 0 {
				t.Errorf("%s suppressed in quiet mode, want output preserved", name)
			}
		})
	}
}

func TestQuietProgressInert(t *testing.T) {
	t.Cleanup(func() { SetQuiet(false) })
	SetQuiet(true)

	var buf bytes.Buffer
	p := NewProgress(&buf, "fetching", 3)
	if p.active {
		t.Error("progress active in quiet mode, want inert")
	}
	p.Increment()
	p.Done()
	if buf.Len() != 0 {
		t.Errorf("inert progress wrote %q, want nothing", buf.String())
	}
}

func TestQuietToggle(t *testing.T) {
	t.Cleanup(func() { SetQuiet(false) })

	SetQuiet(true)
	if !Quiet() {
		t.Error("Quiet() = false after SetQuiet(true)")
	}
	SetQuiet(false)
	if Quiet() {
		t.Error("Quiet() = true after SetQuiet(false)")
	}
}
