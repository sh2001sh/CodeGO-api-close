package auxiliary

import "encoding/binary"

// Container/frame timing supplies measurable admission units without decoding
// audio or guessing duration from compressed byte length.
func compressedDuration(data []byte) float64 {
	if len(data) >= 42 && string(data[:4]) == "fLaC" && data[4]&0x7f == 0 && int(data[5])<<16|int(data[6])<<8|int(data[7]) == 34 {
		packed := binary.BigEndian.Uint64(data[18:26])
		rate := packed >> 44
		samples := packed & 0xfffffffff
		if rate > 0 && samples > 0 {
			return float64(samples) / float64(rate)
		}
	}
	if len(data) >= 27 && string(data[:4]) == "OggS" {
		return oggDuration(data)
	}
	if len(data) >= 7 && data[0] == 0xff && data[1]&0xf6 == 0xf0 {
		return aacDuration(data)
	}
	return mp3Duration(data)
}

func mp3Duration(data []byte) float64 {
	offset := 0
	if len(data) >= 10 && string(data[:3]) == "ID3" {
		for _, value := range data[6:10] {
			if value&0x80 != 0 {
				return 0
			}
		}
		offset = 10 + int(data[6])<<21 + int(data[7])<<14 + int(data[8])<<7 + int(data[9])
		if data[5]&0x10 != 0 {
			offset += 10
		}
	}
	var duration float64
	frames := 0
	for offset+4 <= len(data) {
		if len(data)-offset == 128 && string(data[offset:offset+3]) == "TAG" {
			offset += 128
			break
		}
		header := binary.BigEndian.Uint32(data[offset : offset+4])
		version, layer := (header>>19)&3, (header>>17)&3
		bitrate, sampleIndex := int((header>>12)&15), int((header>>10)&3)
		if header>>21 != 0x7ff || version == 1 || layer != 1 || bitrate == 0 || bitrate == 15 || sampleIndex == 3 {
			return 0
		}
		rate := []int{44100, 48000, 32000}[sampleIndex]
		if version == 2 {
			rate /= 2
		}
		if version == 0 {
			rate /= 4
		}
		bits := []int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}[bitrate]
		coefficient, samples := 144000, 1152
		if version != 3 {
			bits = []int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}[bitrate]
			coefficient, samples = 72000, 576
		}
		length := coefficient*bits/rate + int((header>>9)&1)
		if length < 4 || length > len(data)-offset {
			return 0
		}
		duration += float64(samples) / float64(rate)
		frames++
		offset += length
	}
	if frames == 0 || offset != len(data) {
		return 0
	}
	return duration
}

func aacDuration(data []byte) float64 {
	rates := [...]int{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350}
	offset := 0
	var duration float64
	for offset+7 <= len(data) {
		frame := data[offset:]
		if frame[0] != 0xff || frame[1]&0xf6 != 0xf0 {
			return 0
		}
		index := int((frame[2] >> 2) & 15)
		if index >= len(rates) {
			return 0
		}
		length := int(frame[3]&3)<<11 | int(frame[4])<<3 | int(frame[5]>>5)
		minimum := 7
		if frame[1]&1 == 0 {
			minimum = 9
		}
		if length < minimum || length > len(frame) {
			return 0
		}
		duration += float64(1024*(int(frame[6]&3)+1)) / float64(rates[index])
		offset += length
	}
	if offset != len(data) {
		return 0
	}
	return duration
}

func oggDuration(data []byte) float64 {
	offset, rate, skip := 0, 0, 0
	var serial uint32
	var last uint64
	ended := false
	for offset+27 <= len(data) {
		page := data[offset:]
		if string(page[:4]) != "OggS" || page[4] != 0 {
			return 0
		}
		segments := int(page[26])
		if len(page) < 27+segments {
			return 0
		}
		length := 0
		for _, value := range page[27 : 27+segments] {
			length += int(value)
		}
		start := 27 + segments
		if length > len(page)-start {
			return 0
		}
		if offset == 0 {
			serial = binary.LittleEndian.Uint32(page[14:18])
			payload := page[start : start+length]
			if len(payload) >= 19 && string(payload[:8]) == "OpusHead" {
				rate, skip = 48000, int(binary.LittleEndian.Uint16(payload[10:12]))
			} else if len(payload) >= 16 && payload[0] == 1 && string(payload[1:7]) == "vorbis" {
				rate = int(binary.LittleEndian.Uint32(payload[12:16]))
			} else {
				return 0
			}
		}
		if serial != binary.LittleEndian.Uint32(page[14:18]) || rate <= 0 {
			return 0
		}
		granule := binary.LittleEndian.Uint64(page[6:14])
		if granule != ^uint64(0) {
			last = granule
		}
		ended = page[5]&4 != 0
		offset += start + length
	}
	if offset != len(data) || !ended || last <= uint64(skip) {
		return 0
	}
	return float64(last-uint64(skip)) / float64(rate)
}
