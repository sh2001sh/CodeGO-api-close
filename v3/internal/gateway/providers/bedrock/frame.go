package bedrock

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
)

type frame struct {
	headers map[string]string
	payload []byte
}

func decodeFrame(reader io.Reader) (frame, error) {
	var out frame
	var prelude [12]byte
	if _, err := io.ReadFull(reader, prelude[:]); err != nil {
		return out, err
	}
	length := binary.BigEndian.Uint32(prelude[:4])
	headersLength := binary.BigEndian.Uint32(prelude[4:8])
	if length < 16 || length > maxFrame || headersLength > length-16 {
		return out, fmt.Errorf("invalid eventstream frame length")
	}
	if crc32.ChecksumIEEE(prelude[:8]) != binary.BigEndian.Uint32(prelude[8:]) {
		return out, fmt.Errorf("invalid eventstream prelude checksum")
	}
	data := make([]byte, length-12)
	if _, err := io.ReadFull(reader, data); err != nil {
		return out, err
	}
	crc := crc32.Update(0, crc32.IEEETable, prelude[:])
	crc = crc32.Update(crc, crc32.IEEETable, data[:len(data)-4])
	if crc != binary.BigEndian.Uint32(data[len(data)-4:]) {
		return out, fmt.Errorf("invalid eventstream message checksum")
	}
	var err error
	out.headers, err = decodeHeaders(data[:headersLength])
	if err != nil {
		return out, err
	}
	out.payload = data[headersLength : len(data)-4]
	return out, nil
}

// AWS header values have fixed wire types. We only expose string values, but
// validate and consume every type so an unrelated metadata header is harmless.
func decodeHeaders(data []byte) (map[string]string, error) {
	out := make(map[string]string)
	seen := make(map[string]bool)
	for len(data) > 0 {
		n := int(data[0])
		data = data[1:]
		if n == 0 || len(data) < n+1 {
			return nil, fmt.Errorf("invalid eventstream header name")
		}
		name, kind := string(data[:n]), data[n]
		if seen[name] {
			return nil, fmt.Errorf("duplicate eventstream header")
		}
		seen[name] = true
		data = data[n+1:]
		length := 0
		switch kind {
		case 0, 1: // boolean
		case 2:
			length = 1
		case 3:
			length = 2
		case 4:
			length = 4
		case 5, 8:
			length = 8
		case 9:
			length = 16
		case 6, 7:
			if len(data) < 2 {
				return nil, fmt.Errorf("truncated eventstream header")
			}
			length = int(binary.BigEndian.Uint16(data[:2]))
			data = data[2:]
		default:
			return nil, fmt.Errorf("unknown eventstream header type %d", kind)
		}
		if len(data) < length {
			return nil, fmt.Errorf("truncated eventstream header value")
		}
		if kind == 7 {
			out[name] = string(data[:length])
		}
		data = data[length:]
	}
	return out, nil
}
