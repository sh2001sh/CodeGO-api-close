package auxiliary

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func buildCloudflareAudio(ctx context.Context, req *gateway.Request, target gateway.Target, in Input) (*http.Request, error) {
	model := upstreamModel(req, target)
	if model == "" || strings.ContainsAny(model, "\\?#%") {
		return nil, vectorInvalid("invalid Cloudflare audio model")
	}
	for _, segment := range strings.Split(model, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return nil, vectorInvalid("invalid Cloudflare audio model")
		}
	}
	if _, err := cloudflareAudioFormat(req); err != nil {
		return nil, err
	}
	data, contentType, err := cloudflareAudioFile(in)
	if err != nil {
		return nil, err
	}
	base, secret, err := vectorCloudflareBase(target.BaseURL, target.Secret)
	if err != nil {
		return nil, err
	}
	address, err := endpoint(base, "/run/"+model)
	if err != nil {
		return nil, err
	}
	wire, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	wire.Header.Set("Authorization", "Bearer "+secret)
	wire.Header.Set("Content-Type", contentType)
	return wire, nil
}

func cloudflareAudioFile(in Input) ([]byte, string, error) {
	mediaType, params, err := mime.ParseMediaType(in.ContentType)
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		return nil, "", vectorInvalid("Cloudflare audio requires a multipart file")
	}
	reader := multipart.NewReader(bytes.NewReader(in.Body), params["boundary"])
	var audio []byte
	contentType := ""
	for count := 0; ; count++ {
		if count > 128 {
			return nil, "", vectorInvalid("too many multipart parts")
		}
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", vectorInvalid("malformed audio multipart body")
		}
		if part.FormName() != "file" || part.FileName() == "" {
			continue
		}
		if audio != nil {
			return nil, "", vectorInvalid("only one audio file is supported")
		}
		audio, err = io.ReadAll(io.LimitReader(part, (64<<20)+1))
		if err != nil || len(audio) == 0 || len(audio) > 64<<20 {
			return nil, "", vectorInvalid("audio file is empty or exceeds 64 MiB")
		}
		contentType = part.Header.Get("Content-Type")
		if contentType == "" || contentType == "application/octet-stream" {
			contentType = http.DetectContentType(audio)
		}
		if _, _, err := mime.ParseMediaType(contentType); err != nil {
			return nil, "", vectorInvalid("invalid audio content type")
		}
	}
	if len(audio) == 0 {
		return nil, "", vectorInvalid("audio file is required")
	}
	return audio, contentType, nil
}

func cloudflareAudioFormat(req *gateway.Request) (string, error) {
	format := gjson.GetBytes(req.Body, "response_format").String()
	if format == "" {
		format = "json"
	}
	switch format {
	case "json", "text", "verbose_json", "srt", "vtt":
		return format, nil
	}
	return "", vectorInvalid("unsupported audio response format")
}

func decodeCloudflareAudio(req *gateway.Request, in Input, resp *http.Response) (Response, error) {
	data, err := readResponse(resp)
	if err != nil {
		return Response{}, err
	}
	if err = vectorResponseError(data, resp.StatusCode); err != nil {
		return Response{}, err
	}
	result := gjson.GetBytes(data, "result")
	text := result.Get("text")
	if !result.IsObject() || text.Type != gjson.String || strings.TrimSpace(text.String()) == "" {
		return Response{}, vectorUpstream("invalid_response", "Cloudflare returned no audio text")
	}
	format, err := cloudflareAudioFormat(req)
	if err != nil {
		return Response{}, err
	}
	usage := parseUsage(data)
	if usage == nil {
		usage = parseUsage([]byte(result.Raw))
	}
	if err = vectorUsageError(usage); err != nil {
		return Response{}, err
	}
	headers := vectorJSONHeaders(resp.Header)
	body := []byte(text.String())
	switch format {
	case "json":
		body, err = json.Marshal(map[string]string{"text": text.String()})
	case "text":
		headers.Set("Content-Type", "text/plain; charset=utf-8")
	case "verbose_json":
		task := "transcribe"
		if in.Operation == Translations {
			task = "translate"
		}
		fields := map[string]any{"text": text.String(), "task": task}
		for _, key := range []string{"language", "duration", "words", "segments"} {
			if value := result.Get(key); value.Exists() {
				fields[key] = json.RawMessage(value.Raw)
			}
		}
		body, err = json.Marshal(fields)
	case "srt", "vtt":
		body, err = cloudflareAudioSubtitles(result, format)
		if format == "vtt" {
			headers.Set("Content-Type", "text/vtt; charset=utf-8")
		} else {
			headers.Set("Content-Type", "application/x-subrip; charset=utf-8")
		}
	}
	if err != nil {
		return Response{}, err
	}
	return Response{Body: body, Header: headers, Usage: usage}, nil
}

// Subtitle timestamps come from actual upstream words/segments. A plain text
// response has no timing information, so it cannot be rendered as subtitles.
func cloudflareAudioSubtitles(result gjson.Result, format string) ([]byte, error) {
	segments := result.Get("segments")
	if !segments.IsArray() || len(segments.Array()) == 0 {
		segments = result.Get("words")
	}
	if !segments.IsArray() || len(segments.Array()) == 0 {
		return nil, vectorUpstream("missing_timestamps", "Cloudflare returned no subtitle timestamps")
	}
	var body strings.Builder
	separator := ","
	if format == "vtt" {
		body.WriteString("WEBVTT\n\n")
		separator = "."
	}
	for index, segment := range segments.Array() {
		start, end := segment.Get("start"), segment.Get("end")
		text := segment.Get("text")
		if text.Type != gjson.String {
			text = segment.Get("word")
		}
		if start.Type != gjson.Number || end.Type != gjson.Number || start.Float() < 0 || end.Float() < start.Float() || end.Float() > 24*3600 || text.Type != gjson.String || strings.TrimSpace(text.String()) == "" {
			return nil, vectorUpstream("invalid_timestamps", "Cloudflare returned malformed subtitle timing")
		}
		if format == "srt" {
			fmt.Fprintf(&body, "%d\n", index+1)
		}
		fmt.Fprintf(&body, "%s --> %s\n%s\n\n", cloudflareAudioTimestamp(start.Float(), separator), cloudflareAudioTimestamp(end.Float(), separator), text.String())
	}
	return []byte(body.String()), nil
}

func cloudflareAudioTimestamp(seconds float64, separator string) string {
	millis := int64(math.Round(seconds * 1000))
	return fmt.Sprintf("%02d:%02d:%02d%s%03d", millis/3600000, (millis/60000)%60, (millis/1000)%60, separator, millis%1000)
}
