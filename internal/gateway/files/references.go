package files

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"

	gatewaySchema "github.com/sh2001sh/new-api/internal/gateway/schema"
	"github.com/sh2001sh/new-api/types"
)

type InputMode string

const (
	InputModeAuto      InputMode = "auto"
	InputModeNative    InputMode = "native"
	InputModeSignedURL InputMode = "signed_url"
	InputModeBase64    InputMode = "base64"
)

type PrepareOptions struct {
	UserID       int
	Admin        bool
	Mode         InputMode
	NativeUpload func(*gatewaySchema.UserFile) (string, error)
	SignedURL    func(*gatewaySchema.UserFile) (string, error)
	OnFallback   func(InputMode, error)
}

type preparedFile struct {
	file         *gatewaySchema.UserFile
	dataURL      string
	nativeID     string
	signedURL    string
	nativeTried  bool
	signedTried  bool
	nativeErr    error
	signedURLErr error
}

func NormalizeInputMode(raw string) InputMode {
	switch InputMode(raw) {
	case InputModeNative, InputModeSignedURL, InputModeBase64:
		return InputMode(raw)
	default:
		return InputModeAuto
	}
}

func ContainsLocalFileIDs(raw []byte) bool {
	return bytes.Contains(raw, []byte(types.LocalFileIDPrefix))
}

// ValidateFileIDsJSON verifies ownership and refreshes referenced local files
// without changing the request body used by later channel attempts.
func ValidateFileIDsJSON(raw []byte, userID int, admin bool) (bool, error) {
	if !ContainsLocalFileIDs(raw) {
		return false, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, err
	}
	ids := make(map[string]struct{})
	collectLocalFileIDs(value, ids)
	for id := range ids {
		file, err := Get(id, userID, admin)
		if err != nil {
			return false, fmt.Errorf("validate file_id %s: %w", id, err)
		}
		if err := MarkUsed(file); err != nil {
			return false, fmt.Errorf("refresh file_id %s: %w", id, err)
		}
	}
	return len(ids) > 0, nil
}

func collectLocalFileIDs(value any, ids map[string]struct{}) {
	switch current := value.(type) {
	case []any:
		for _, child := range current {
			collectLocalFileIDs(child, ids)
		}
	case map[string]any:
		if id, ok := current["file_id"].(string); ok && types.IsLocalFileID(id) {
			ids[id] = struct{}{}
		}
		for _, child := range current {
			collectLocalFileIDs(child, ids)
		}
	}
}

// PrepareFileIDsJSON chooses the cheapest representation supported by the
// selected upstream attempt. It never mutates the caller's source bytes.
func PrepareFileIDsJSON(raw []byte, options PrepareOptions) ([]byte, error) {
	if !ContainsLocalFileIDs(raw) {
		return raw, nil
	}
	options.Mode = NormalizeInputMode(string(options.Mode))
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	cache := make(map[string]*preparedFile)
	if err := prepareValue(&value, "", options, cache); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

// ResolveFileIDsJSON preserves the original Base64-only API for callers that
// do not have a selected upstream channel.
func ResolveFileIDsJSON(raw []byte, userID int, admin bool) ([]byte, error) {
	return PrepareFileIDsJSON(raw, PrepareOptions{UserID: userID, Admin: admin, Mode: InputModeBase64})
}

func prepareValue(value *any, parentKey string, options PrepareOptions, cache map[string]*preparedFile) error {
	switch current := (*value).(type) {
	case []any:
		for i := range current {
			if err := prepareValue(&current[i], parentKey, options, cache); err != nil {
				return err
			}
		}
	case map[string]any:
		if id, ok := current["file_id"].(string); ok && types.IsLocalFileID(id) {
			if err := prepareReference(current, parentKey, id, options, cache); err != nil {
				return err
			}
		}
		for key, child := range current {
			if err := prepareValue(&child, key, options, cache); err != nil {
				return err
			}
			current[key] = child
		}
	}
	return nil
}

func prepareReference(current map[string]any, parentKey, id string, options PrepareOptions, cache map[string]*preparedFile) error {
	prepared, err := loadPreparedFile(id, options, cache)
	if err != nil {
		return err
	}
	typ, _ := current["type"].(string)
	if supportsNativeReference(typ, parentKey) && shouldTryNative(options.Mode) {
		if upstreamID, err := prepared.upstreamID(options); err == nil && upstreamID != "" {
			current["file_id"] = upstreamID
			return nil
		} else if err != nil && options.OnFallback != nil {
			options.OnFallback(InputModeNative, err)
		}
	}
	if supportsURLReference(typ, parentKey) && shouldTrySignedURL(options.Mode) {
		if signedURL, err := prepared.deliveryURL(options); err == nil && signedURL != "" {
			applyURLReference(current, parentKey, typ, signedURL)
			return nil
		} else if err != nil && options.OnFallback != nil {
			options.OnFallback(InputModeSignedURL, err)
		}
	}
	return prepared.applyBase64(current, parentKey, typ)
}

func loadPreparedFile(id string, options PrepareOptions, cache map[string]*preparedFile) (*preparedFile, error) {
	if cached := cache[id]; cached != nil {
		return cached, nil
	}
	file, err := Get(id, options.UserID, options.Admin)
	if err != nil {
		return nil, fmt.Errorf("resolve file_id %s: %w", id, err)
	}
	if err := MarkUsed(file); err != nil {
		return nil, err
	}
	prepared := &preparedFile{file: file}
	cache[id] = prepared
	return prepared, nil
}

func (file *preparedFile) upstreamID(options PrepareOptions) (string, error) {
	if !file.nativeTried {
		file.nativeTried = true
		if options.NativeUpload == nil {
			file.nativeErr = fmt.Errorf("native file upload is unavailable")
		} else {
			file.nativeID, file.nativeErr = options.NativeUpload(file.file)
		}
	}
	return file.nativeID, file.nativeErr
}

func (file *preparedFile) deliveryURL(options PrepareOptions) (string, error) {
	if !file.signedTried {
		file.signedTried = true
		if options.SignedURL == nil {
			file.signedURLErr = fmt.Errorf("signed file delivery is unavailable")
		} else {
			file.signedURL, file.signedURLErr = options.SignedURL(file.file)
		}
	}
	return file.signedURL, file.signedURLErr
}

func (file *preparedFile) applyBase64(current map[string]any, parentKey, typ string) error {
	if file.dataURL == "" {
		content, err := ReadContent(file.file)
		if err != nil {
			return err
		}
		file.dataURL = "data:" + file.file.MimeType + ";base64," + base64.StdEncoding.EncodeToString(content)
	}
	applyDataReference(current, parentKey, typ, file.dataURL, file.file.Filename)
	return nil
}

func supportsNativeReference(typ, parentKey string) bool {
	return typ == "input_image" || typ == "input_file" || parentKey == "file"
}

func supportsURLReference(typ, parentKey string) bool {
	return typ == "input_image" || typ == "input_file" || typ == "image_url" || parentKey == "image_url"
}

func shouldTryNative(mode InputMode) bool {
	return mode == InputModeAuto || mode == InputModeNative
}

func shouldTrySignedURL(mode InputMode) bool {
	return mode == InputModeAuto || mode == InputModeSignedURL
}

func applyURLReference(current map[string]any, parentKey, typ, value string) {
	switch {
	case typ == "input_image" || typ == "image_url":
		current["image_url"] = value
	case parentKey == "image_url":
		current["url"] = value
	case typ == "input_file":
		current["file_url"] = value
	}
	delete(current, "file_id")
}

func applyDataReference(current map[string]any, parentKey, typ, value, filename string) {
	switch {
	case typ == "input_image" || typ == "image_url":
		current["image_url"] = value
	case parentKey == "image_url":
		current["url"] = value
	default:
		current["file_data"] = value
		if _, exists := current["filename"]; !exists {
			current["filename"] = filename
		}
	}
	delete(current, "file_id")
}
