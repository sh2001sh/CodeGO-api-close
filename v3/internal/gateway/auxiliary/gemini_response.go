package auxiliary

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type geminiVector struct {
	Values []float64 `json:"values"`
}

type geminiAuxResponse struct {
	Embedding   *geminiVector  `json:"embedding"`
	Embeddings  []geminiVector `json:"embeddings"`
	Predictions []struct {
		Data     string `json:"bytesBase64Encoded"`
		Filtered string `json:"raiFilteredReason"`
	} `json:"predictions"`
	Error *struct {
		Code    int    `json:"code"`
		Status  string `json:"status"`
		Message string `json:"message"`
	} `json:"error"`
}

func (geminiNativeAdapter) Decode(_ context.Context, req *gateway.Request, _ gateway.Target, input Input, resp *http.Response) (Response, error) {
	if resp.StatusCode >= 300 {
		return rawResponse(resp)
	}
	data, err := readResponse(resp)
	if err != nil {
		return Response{}, err
	}
	if _, err = object(data); err != nil {
		return Response{}, fmt.Errorf("gemini returned an invalid response: %w", err)
	}
	var native geminiAuxResponse
	if err = json.Unmarshal(data, &native); err != nil {
		return Response{}, fmt.Errorf("gemini returned an invalid response: %w", err)
	}
	if native.Error != nil {
		status := native.Error.Code
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		return Response{}, &gateway.UpstreamError{Status: status, Type: "upstream_error", Code: native.Error.Status, Message: native.Error.Message}
	}
	response := Response{Body: data, Header: resp.Header.Clone(), Usage: parseUsage(data)}
	switch input.Operation {
	case GeminiEmbed:
		err = decodeGeminiSingleEmbed(native)
	case Embeddings, GeminiBatchEmbed:
		err = decodeGeminiEmbeddings(req, input, native, &response)
	case Images, GeminiImages:
		err = decodeGeminiImages(input, native, &response)
	default:
		return Response{}, unsupported(input.Operation)
	}
	if err != nil {
		return Response{}, err
	}
	if input.Operation == Embeddings || input.Operation == Images {
		response.Header.Set("Content-Type", "application/json")
		response.Header.Del("Content-Length")
		response.Header.Del("Content-Encoding")
		response.Header.Del("ETag")
		response.Header.Del("Content-MD5")
	}
	return response, nil
}

// decodeGeminiSingleEmbed validates a native Gemini single-embedding
// response for the GeminiEmbed operation.
func decodeGeminiSingleEmbed(native geminiAuxResponse) error {
	if native.Embedding == nil || len(native.Embedding.Values) == 0 {
		return fmt.Errorf("gemini returned no embedding")
	}
	return nil
}

// decodeGeminiEmbeddings validates a native Gemini batch-embedding response
// and, for the OpenAI-compatible Embeddings operation, rewrites the response
// body into the OpenAI embeddings format.
func decodeGeminiEmbeddings(req *gateway.Request, input Input, native geminiAuxResponse, response *Response) error {
	if len(native.Embeddings) == 0 {
		return fmt.Errorf("gemini returned no embeddings")
	}
	for _, vector := range native.Embeddings {
		if len(vector.Values) == 0 {
			return fmt.Errorf("gemini returned an empty embedding")
		}
	}
	if input.Operation != Embeddings {
		var requested struct {
			Requests []json.RawMessage `json:"requests"`
		}
		if json.Unmarshal(input.Body, &requested) != nil || len(requested.Requests) != len(native.Embeddings) {
			return fmt.Errorf("gemini embedding count does not match request count")
		}
		return nil
	}
	body, err := geminiOpenAIEmbeddings(req, input, native.Embeddings, response.Usage)
	if err != nil {
		return err
	}
	response.Body = body
	return nil
}

// decodeGeminiImages validates generated images from a native Gemini
// predictions response and, for the OpenAI-compatible Images operation,
// rewrites the response body into the OpenAI images format.
func decodeGeminiImages(input Input, native geminiAuxResponse, response *Response) error {
	images := make([]map[string]string, 0, len(native.Predictions))
	for _, image := range native.Predictions {
		if image.Filtered != "" {
			continue
		}
		if decoded, decodeErr := base64.StdEncoding.DecodeString(image.Data); decodeErr != nil || len(decoded) == 0 {
			return fmt.Errorf("gemini returned invalid image data")
		}
		images = append(images, map[string]string{"b64_json": image.Data})
	}
	if len(images) == 0 {
		return &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error", Code: "content_filter", Message: "Gemini returned no generated images"}
	}
	response.Header.Set("X-Codego-Image-Count", strconv.Itoa(len(images)))
	if response.Usage == nil {
		// Keep the legacy 258 input-token tariff per generated image. It is an
		// estimate, and the image output dimension remains available to rules.
		tokens := int64(len(images)) * 258
		response.Usage = &gateway.Usage{PromptTokens: tokens, ImageOutputTokens: tokens, Estimated: true}
	}
	if input.Operation != Images {
		return nil
	}
	body, err := json.Marshal(map[string]any{"created": time.Now().Unix(), "data": images})
	if err != nil {
		return err
	}
	response.Body = body
	return nil
}

func geminiOpenAIEmbeddings(req *gateway.Request, input Input, vectors []geminiVector, usage *gateway.Usage) ([]byte, error) {
	var body struct {
		Input      json.RawMessage `json:"input"`
		Encoding   string          `json:"encoding_format"`
		Dimensions int             `json:"dimensions"`
	}
	if err := json.Unmarshal(input.Body, &body); err != nil {
		return nil, err
	}
	var inputs []string
	var single string
	if json.Unmarshal(body.Input, &single) == nil {
		inputs = []string{single}
	} else {
		if err := json.Unmarshal(body.Input, &inputs); err != nil {
			return nil, err
		}
	}
	if len(inputs) != len(vectors) {
		return nil, fmt.Errorf("gemini embedding count does not match input count")
	}
	items := make([]map[string]any, len(vectors))
	for i, vector := range vectors {
		if body.Dimensions > 0 && body.Dimensions != len(vector.Values) {
			return nil, fmt.Errorf("gemini embedding dimensions do not match requested dimensions")
		}
		var embedding any = vector.Values
		if body.Encoding == "base64" {
			bytes := make([]byte, len(vector.Values)*4)
			for j, value := range vector.Values {
				converted := float32(value)
				if math.IsInf(float64(converted), 0) {
					return nil, fmt.Errorf("gemini embedding exceeds float32 range")
				}
				binary.LittleEndian.PutUint32(bytes[j*4:], math.Float32bits(converted))
			}
			embedding = base64.StdEncoding.EncodeToString(bytes)
		}
		items[i] = map[string]any{"object": "embedding", "index": i, "embedding": embedding}
	}
	result := map[string]any{"object": "list", "model": req.Model, "data": items}
	if usage != nil {
		result["usage"] = map[string]int64{"prompt_tokens": usage.PromptTokens, "total_tokens": usage.PromptTokens + usage.CompletionTokens}
	}
	return json.Marshal(result)
}
