package zhipu

import (
	"bufio"
	"bytes"
	"errors"
	"io"

	"github.com/sh2001sh/new-api/v3/pkg/sse"
)

type nativeEvent struct {
	name string
	data []byte
	meta []byte
}

// Native v3 adds a nonstandard meta: field to its finish event.
type nativeReader struct{ reader *bufio.Reader }

func (r *nativeReader) Next() (nativeEvent, error) {
	var event nativeEvent
	fields, dataLines, size := 0, 0, 0
	for {
		line, err := r.readLine()
		if err != nil {
			if errors.Is(err, io.EOF) && fields > 0 {
				err = io.ErrUnexpectedEOF
			}
			return nativeEvent{}, err
		}
		size += len(line)
		if size > maxEvent {
			return nativeEvent{}, sse.ErrEventTooLarge
		}
		if len(line) == 0 {
			if fields > 0 {
				return event, nil
			}
			size = 0
			continue
		}
		if line[0] == ':' {
			continue
		}
		key, value, _ := bytes.Cut(line, []byte(":"))
		value = bytes.TrimPrefix(value, []byte(" "))
		switch string(key) {
		case "event":
			event.name = string(value)
		case "data":
			if dataLines > 0 {
				event.data = append(event.data, '\n')
			}
			event.data = append(event.data, value...)
			dataLines++
		case "meta":
			if event.meta != nil {
				return nativeEvent{}, errors.New("zhipu: duplicate stream metadata")
			}
			event.meta = append([]byte{}, value...)
		}
		fields++
	}
}

func (r *nativeReader) readLine() ([]byte, error) {
	var line []byte
	for {
		part, err := r.reader.ReadSlice('\n')
		if len(line)+len(part) > maxEvent {
			return nil, sse.ErrEventTooLarge
		}
		line = append(line, part...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil && (!errors.Is(err, io.EOF) || len(line) == 0) {
			return nil, err
		}
		line = bytes.TrimSuffix(line, []byte{'\n'})
		return bytes.TrimSuffix(line, []byte{'\r'}), nil
	}
}
