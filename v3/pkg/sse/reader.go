// Package sse reads upstream Server-Sent Events and writes them to clients.
package sse

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

// DefaultMaxEventSize bounds a single event so a broken upstream cannot grow
// memory without limit.
const DefaultMaxEventSize = 8 << 20

// ErrEventTooLarge reports an event larger than the reader's limit.
var ErrEventTooLarge = errors.New("sse: event too large")

// Event is one parsed SSE event. Its slices are only valid until the next
// call to Reader.Next; copy them if they must outlive that call.
type Event struct {
	Name []byte // "event:" field; empty means the default "message"
	ID   []byte
	Data []byte // "data:" lines joined with '\n'
}

// Reader parses an SSE stream into events without allocating per event.
type Reader struct {
	br      *bufio.Reader
	maxSize int
	data    []byte
	name    []byte
	id      []byte
}

// NewReader returns a Reader. maxSize <= 0 selects DefaultMaxEventSize.
func NewReader(r io.Reader, maxSize int) *Reader {
	if maxSize <= 0 {
		maxSize = DefaultMaxEventSize
	}
	return &Reader{br: bufio.NewReaderSize(r, 64<<10), maxSize: maxSize}
}

// Next returns the next event. It returns io.EOF after the last complete
// event, or io.ErrUnexpectedEOF if the stream ended mid-event.
func (r *Reader) Next() (Event, error) {
	r.data, r.name, r.id = r.data[:0], r.name[:0], r.id[:0]
	hasField := false
	dataLines := 0
	for {
		line, err := r.readLine()
		if err != nil {
			if errors.Is(err, io.EOF) && hasField {
				return Event{}, io.ErrUnexpectedEOF
			}
			return Event{}, err
		}
		if len(line) == 0 {
			if hasField {
				return Event{Name: r.name, ID: r.id, Data: r.data}, nil
			}
			continue // blank lines between events
		}
		if line[0] == ':' {
			continue // comment / keep-alive
		}
		field, value := splitField(line)
		switch string(field) {
		case "data":
			if dataLines > 0 {
				r.data = append(r.data, '\n')
			}
			if len(r.data)+len(value) > r.maxSize {
				return Event{}, ErrEventTooLarge
			}
			r.data = append(r.data, value...)
			dataLines++
		case "event":
			r.name = append(r.name, value...)
		case "id":
			r.id = append(r.id, value...)
		default:
			// "retry" and unknown fields are ignored per the SSE spec.
		}
		hasField = true
	}
}

// readLine returns one line without its terminator. The returned slice is
// valid until the next read.
func (r *Reader) readLine() ([]byte, error) {
	line, err := r.br.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		// Rare long line: accumulate, still bounded by maxSize.
		long := append([]byte(nil), line...)
		for errors.Is(err, bufio.ErrBufferFull) {
			line, err = r.br.ReadSlice('\n')
			if len(long)+len(line) > r.maxSize {
				return nil, ErrEventTooLarge
			}
			long = append(long, line...)
		}
		line = long
	}
	// A final line without '\n' is still data; EOF is reported on the next read.
	lastLine := errors.Is(err, io.EOF) && len(line) > 0
	if err != nil && !lastLine {
		return nil, err
	}
	line = bytes.TrimSuffix(line, []byte{'\n'})
	return bytes.TrimSuffix(line, []byte{'\r'}), nil
}

func splitField(line []byte) (field, value []byte) {
	i := bytes.IndexByte(line, ':')
	if i < 0 {
		return line, nil
	}
	value = line[i+1:]
	if len(value) > 0 && value[0] == ' ' {
		value = value[1:]
	}
	return line[:i], value
}
