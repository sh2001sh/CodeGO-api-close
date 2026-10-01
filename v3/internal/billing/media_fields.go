package billing

// Exact non-token dimensions are preserved on billing events and their ledger
// metadata. Durations use microseconds; character counts use Unicode code points.
const (
	FieldImageCount          = "image_count"
	FieldAudioDurationMicros = "audio_duration_micros"
	FieldAudioCharacters     = "audio_characters"
	FieldVideoDurationMicros = "video_duration_micros"
)
