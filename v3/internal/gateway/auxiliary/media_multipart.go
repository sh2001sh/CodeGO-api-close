package auxiliary

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
)

func mediaMultipartImages(in Input) ([]string, error) {
	mediaType, params, err := mime.ParseMediaType(in.ContentType)
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		return nil, errors.New("auxiliary: image edit requires multipart images")
	}
	reader := multipart.NewReader(bytes.NewReader(in.Body), params["boundary"])
	var images []string
	for count := 0; ; count++ {
		if count > 128 {
			return nil, errors.New("auxiliary: too many image parts")
		}
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		name := part.FormName()
		if part.FileName() == "" || (name != "image" && !strings.HasPrefix(name, "image[")) {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(part, (32<<20)+1))
		if err != nil {
			return nil, err
		}
		if len(data) == 0 || len(data) > 32<<20 {
			return nil, errors.New("auxiliary: invalid image size")
		}
		images = append(images, "data:"+http.DetectContentType(data)+";base64,"+base64.StdEncoding.EncodeToString(data))
	}
	if len(images) == 0 {
		return nil, errors.New("auxiliary: image is required")
	}
	return images, nil
}
