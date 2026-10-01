package auxiliary

import (
	"encoding/binary"
	"math"
	"testing"
)

func oggPage(serial uint32, granule uint64, flags byte, payload []byte) []byte {
	page := make([]byte, 28+len(payload))
	copy(page, "OggS")
	page[5], page[26], page[27] = flags, 1, byte(len(payload))
	binary.LittleEndian.PutUint64(page[6:14], granule)
	binary.LittleEndian.PutUint32(page[14:18], serial)
	copy(page[28:], payload)
	return page
}

func TestCompressedAudioUsesFrameAndContainerTiming(t *testing.T) {
	mp3 := make([]byte, 417)
	copy(mp3, []byte{0xff, 0xfb, 0x90, 0}) // MPEG1 Layer III, 128kbps, 44.1kHz.
	id3mp3 := append([]byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 3, 0, 0, 0}, mp3...)
	aac := make([]byte, 10)
	copy(aac, []byte{0xff, 0xf1, 0x50, 0x80, 0x01, 0x5f, 0xfc}) // ADTS 44.1kHz, 10-byte frame.
	flac := make([]byte, 43)
	copy(flac, "fLaC")
	flac[4], flac[7] = 0x80, 34
	binary.BigEndian.PutUint64(flac[18:26], uint64(48000)<<44|96000)
	opus := make([]byte, 19)
	copy(opus, "OpusHead")
	opus[8], opus[9] = 1, 1
	binary.LittleEndian.PutUint16(opus[10:12], 312)
	oggOpus := append(oggPage(1, 0, 2, opus), oggPage(1, 48312, 4, []byte{1})...)
	vorbis := make([]byte, 30)
	vorbis[0] = 1
	copy(vorbis[1:], "vorbis")
	vorbis[11] = 1
	binary.LittleEndian.PutUint32(vorbis[12:16], 48000)
	oggVorbis := append(oggPage(2, 0, 2, vorbis), oggPage(2, 96000, 4, []byte{1})...)
	for _, test := range []struct {
		name    string
		data    []byte
		seconds float64
	}{
		{"mp3", mp3, 1152.0 / 44100}, {"id3 mp3", id3mp3, 1152.0 / 44100},
		{"aac", aac, 1024.0 / 44100}, {"flac", flac, 2}, {"ogg opus", oggOpus, 1}, {"ogg vorbis", oggVorbis, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			if seconds := audioDuration(test.data, ""); math.Abs(seconds-test.seconds) > 1e-10 {
				t.Fatalf("seconds=%f want=%f", seconds, test.seconds)
			}
		})
	}
	for name, data := range map[string][]byte{
		"truncated mp3": mp3[:len(mp3)-1], "truncated aac": aac[:9], "invalid mp3": {0xff, 0xfb, 0xf0, 0},
		"invalid id3": {'I', 'D', '3', 4, 0, 0, 0x80, 0, 0, 0}, "truncated flac": flac[:24],
		"truncated ogg": oggOpus[:len(oggOpus)-1], "no EOS": oggPage(1, 48000, 2, opus), "empty": nil,
	} {
		t.Run(name, func(t *testing.T) {
			if seconds := audioDuration(data, ""); seconds != 0 {
				t.Fatalf("invalid data duration=%f", seconds)
			}
		})
	}
}

func TestWAVDurationRejectsMalformedFormatAndTruncation(t *testing.T) {
	if seconds := audioDuration(testWAV(), ""); seconds != 1.25 {
		t.Fatalf("forged ByteRate changed duration=%f", seconds)
	}
	for _, mutate := range []func([]byte){
		func(data []byte) { binary.LittleEndian.PutUint16(data[22:24], 0) },
		func(data []byte) { binary.LittleEndian.PutUint32(data[24:28], 0) },
		func(data []byte) { binary.LittleEndian.PutUint16(data[34:36], 15) },
		func(data []byte) { binary.LittleEndian.PutUint32(data[40:44], ^uint32(0)) },
	} {
		data := testWAV()
		mutate(data)
		if seconds := audioDuration(data, ""); seconds != 0 {
			t.Fatalf("malformed WAV duration=%f", seconds)
		}
	}
	if seconds := audioDuration(testWAV()[:100], ""); seconds != 0 {
		t.Fatalf("truncated WAV duration=%f", seconds)
	}
}
