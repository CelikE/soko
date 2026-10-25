package picker

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// chunkReader preserves the requested read boundaries, including short reads.
type chunkReader struct {
	chunks []string
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if len(r.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.chunks[0])
	r.chunks[0] = r.chunks[0][n:]
	if r.chunks[0] == "" {
		r.chunks = r.chunks[1:]
	}
	return n, nil
}

func TestKeyReaderReadBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name   string
		chunks []string
		want   []string
	}{
		{name: "split escape", chunks: []string{"\x1b", "[B"}, want: []string{"\x1b[B"}},
		{name: "split final byte", chunks: []string{"\x1b[", "A"}, want: []string{"\x1b[A"}},
		{name: "split every byte", chunks: []string{"\x1b", "[", "B"}, want: []string{"\x1b[B"}},
		{name: "combined arrows and enter", chunks: []string{"\x1b[B\x1b[A\r"}, want: []string{"\x1b[B", "\x1b[A", "\r"}},
		{name: "combined search and control keys", chunks: []string{"api\x7f\b \r\n\x03"}, want: []string{"a", "p", "i", "\x7f", "\b", " ", "\r", "\n", "\x03"}},
		{name: "application mode arrows", chunks: []string{"\x1b", "OB\x1bO", "A"}, want: []string{"\x1b[B", "\x1b[A"}},
		{name: "unsupported CSI", chunks: []string{"\x1b", "[1;", "5Cx"}, want: []string{"", "x"}},
		{name: "unsupported SS3", chunks: []string{"\x1bOPx"}, want: []string{"", "x"}},
		{name: "escape before printable", chunks: []string{"\x1bx"}, want: []string{"\x1b", "x"}},
		{name: "repeated escape", chunks: []string{"\x1b\x1b"}, want: []string{"\x1b", "\x1b"}},
		{name: "escape at EOF", chunks: []string{"\x1b"}, want: []string{"\x1b"}},
		{name: "incomplete CSI at EOF", chunks: []string{"\x1b["}, want: []string{""}},
		{name: "incomplete SS3 at EOF", chunks: []string{"\x1bO"}, want: []string{""}},
		{name: "control interrupts sequence", chunks: []string{"\x1b[\x03"}, want: []string{"", "\x03"}},
		{name: "escape interrupts sequence", chunks: []string{"\x1b[\x1b[B"}, want: []string{"", "\x1b[B"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			keys, err := newKeyReader(&chunkReader{chunks: tt.chunks})
			if err != nil {
				t.Fatal(err)
			}
			defer keys.close()
			for i, want := range tt.want {
				got, err := keys.next()
				if err != nil || got != want {
					t.Fatalf("key %d = %q, %v; want %q, nil", i, got, err, want)
				}
			}
			if got, err := keys.next(); !errors.Is(err, io.EOF) {
				t.Fatalf("after final key = %q, %v; want EOF", got, err)
			}
		})
	}
}

func pipeKeys(t *testing.T) (*keyReader, *os.File) {
	t.Helper()
	in, out, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	keys, err := newKeyReader(in)
	if err != nil {
		_ = in.Close()
		_ = out.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Also unblock readers on platforms where pipe cancellation falls back
		// to plain io.Reader behavior (native Windows).
		_ = out.Close()
		_ = in.Close()
		keys.close()
	})
	return keys, out
}

func TestKeyReaderWaitsForDelayedArrow(t *testing.T) {
	keys, out := pipeKeys(t)
	written := make(chan error, 1)
	go func() {
		for _, chunk := range []string{"\x1b", "[", "B\r"} {
			if _, err := out.WriteString(chunk); err != nil {
				written <- err
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		written <- nil
	}()
	for _, want := range []string{"\x1b[B", "\r"} {
		if got, err := keys.next(); err != nil || got != want {
			t.Fatalf("key = %q, %v; want %q, nil", got, err, want)
		}
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
}

func TestKeyReaderEscapeTimeout(t *testing.T) {
	for _, prefix := range []string{"\x1b", "\x1b[", "\x1bO"} {
		t.Run(prefix, func(t *testing.T) {
			keys, out := pipeKeys(t)
			if _, err := out.WriteString(prefix); err != nil {
				t.Fatal(err)
			}
			want := ""
			if prefix == "\x1b" {
				want = prefix
			}
			if got, err := keys.next(); err != nil || got != want {
				t.Fatalf("key = %q, %v; want %q, nil", got, err, want)
			}
			// The timed-out read must remain reusable rather than losing the
			// next key, returning another Escape, or blocking in a second read.
			if _, err := out.WriteString("x\r"); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"x", "\r"} {
				if got, err := keys.next(); err != nil || got != want {
					t.Fatalf("after timeout = %q, %v; want %q, nil", got, err, want)
				}
			}
		})
	}
}

type finalErrorReader struct {
	data string
	err  error
}

func (r *finalErrorReader) Read(p []byte) (int, error) {
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, r.err
}

func TestKeyReaderPreservesReadErrorsAndFinalData(t *testing.T) {
	broken := errors.New("broken input")
	for _, err := range []error{io.EOF, broken} {
		t.Run(err.Error(), func(t *testing.T) {
			keys, newErr := newKeyReader(&finalErrorReader{data: "a\r", err: err})
			if newErr != nil {
				t.Fatal(newErr)
			}
			defer keys.close()
			for _, want := range []string{"a", "\r"} {
				if got, readErr := keys.next(); readErr != nil || got != want {
					t.Fatalf("key = %q, %v; want %q, nil", got, readErr, want)
				}
			}
			if _, readErr := keys.next(); !errors.Is(readErr, err) {
				t.Fatalf("final error = %v, want %v", readErr, err)
			}
		})
	}
	keys, err := newKeyReader(&finalErrorReader{data: "\x1b", err: broken})
	if err != nil {
		t.Fatal(err)
	}
	defer keys.close()
	if _, err := keys.next(); !errors.Is(err, broken) {
		t.Fatalf("sequence read error = %v, want %v", err, broken)
	}
}

func TestKeyReaderLongUnsupportedSequence(t *testing.T) {
	keys, err := newKeyReader(strings.NewReader("\x1b[" + strings.Repeat("1;", 300) + "Cx"))
	if err != nil {
		t.Fatal(err)
	}
	defer keys.close()
	for _, want := range []string{"", "x"} {
		if got, err := keys.next(); err != nil || got != want {
			t.Fatalf("key = %q, %v; want %q, nil", got, err, want)
		}
	}
}
