package picker

import (
	"errors"
	"io"
	"time"

	"github.com/muesli/cancelreader"
)

// escapeDelay distinguishes a standalone Escape from a terminal key sequence.
// Only sequence continuations use a timeout; ordinary input can wait forever.
const escapeDelay = 100 * time.Millisecond

var errKeyTimeout = errors.New("key continuation timed out")

type inputRead struct {
	data []byte
	err  error
}

type keyReader struct {
	reader  cancelreader.CancelReader
	pending []byte
	readErr error
	reading chan inputRead
}

func newKeyReader(in io.Reader) (*keyReader, error) {
	reader, err := cancelreader.NewReader(in)
	if err != nil {
		return nil, err
	}
	return &keyReader{reader: reader}, nil
}

func (r *keyReader) close() {
	if r.reading != nil && r.reader.Cancel() {
		// Finish the outstanding read before releasing its polling resources.
		<-r.reading
	}
	_ = r.reader.Close()
}

func (r *keyReader) readByte(timeout <-chan time.Time) (byte, error) {
	for len(r.pending) == 0 {
		if r.readErr != nil {
			return 0, r.readErr
		}
		if r.reading == nil {
			result := make(chan inputRead, 1)
			r.reading = result
			// Read on demand, never ahead of the decoder. After selection no
			// background reader should consume input intended for the shell.
			go func() {
				var buf [256]byte
				n, err := r.reader.Read(buf[:])
				result <- inputRead{data: buf[:n], err: err}
			}()
		}
		select {
		case result := <-r.reading:
			r.pending, r.readErr = result.data, result.err
			r.reading = nil
		case <-timeout:
			return 0, errKeyTimeout
		}
	}
	b := r.pending[0]
	r.pending = r.pending[1:]
	return b, nil
}

// next returns one key, regardless of how reads split or combine its bytes.
// Unsupported terminal sequences are consumed as a whole and return "".
func (r *keyReader) next() (string, error) {
	b, err := r.readByte(nil)
	if err != nil {
		return "", err
	}
	if b != '\x1b' {
		return string(b), nil
	}

	timer := time.NewTimer(escapeDelay)
	defer timer.Stop()
	b, err = r.readByte(timer.C)
	if errors.Is(err, errKeyTimeout) || errors.Is(err, io.EOF) {
		return "\x1b", nil
	}
	if err != nil {
		return "", err
	}
	if b != '[' && b != 'O' {
		// Escape followed by a separate key: leave that key for the next call.
		r.pending = append([]byte{b}, r.pending...)
		return "\x1b", nil
	}

	for length := 3; ; length++ {
		b, err = r.readByte(timer.C)
		if errors.Is(err, errKeyTimeout) || errors.Is(err, io.EOF) {
			return "", nil // incomplete sequence must not become search text
		}
		if err != nil {
			return "", err
		}
		if b >= 0x40 && b <= 0x7e { // final byte of CSI or SS3 sequence
			if length == 3 && (b == 'A' || b == 'B') {
				return "\x1b[" + string(b), nil
			}
			return "", nil
		}
		if b < 0x20 || b > 0x3f {
			r.pending = append([]byte{b}, r.pending...)
			return "", nil
		}
	}
}
