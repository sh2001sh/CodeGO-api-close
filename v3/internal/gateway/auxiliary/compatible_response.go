package auxiliary

import "github.com/tidwall/gjson"

func validateCompatibleResponse(op Operation, body []byte) error {
	switch op {
	case Images, ImageEdits:
		data := gjson.GetBytes(body, "data")
		if !data.IsArray() || len(data.Array()) == 0 {
			return failure(502, "empty_response", "upstream returned no images")
		}
		for _, item := range data.Array() {
			if item.Get("url").String() == "" && item.Get("b64_json").String() == "" {
				return failure(502, "invalid_response", "upstream returned an invalid image")
			}
		}
	case Embeddings:
		data := gjson.GetBytes(body, "data")
		if !data.IsArray() || len(data.Array()) == 0 {
			return failure(502, "empty_response", "upstream returned no embeddings")
		}
		for _, item := range data.Array() {
			value := item.Get("embedding")
			if value.Type == gjson.String && value.String() != "" {
				continue
			}
			if !value.IsArray() || len(value.Array()) == 0 {
				return failure(502, "invalid_response", "upstream returned an invalid embedding")
			}
			for _, number := range value.Array() {
				if number.Type != gjson.Number {
					return failure(502, "invalid_response", "upstream embedding must contain numbers")
				}
			}
		}
	case Completions, Edits:
		if !streamHasOutput(gjson.ParseBytes(body)) {
			return failure(502, "empty_response", "upstream returned no text")
		}
	}
	return nil
}
