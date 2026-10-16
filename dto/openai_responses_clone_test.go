package dto

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	platformcopy "github.com/sh2001sh/new-api/internal/platform/copyx"
	"github.com/tiktoken-go/tokenizer/codec"
)

// Populate every field, including newly added fields. An unhandled mutable type
// fails explicitly instead of silently leaving the clone's coverage incomplete.
func populateResponsesCloneFixture(t testing.TB, value reflect.Value, path string) {
	t.Helper()
	switch value.Kind() {
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			populateResponsesCloneFixture(t, value.Field(i), path+"."+value.Type().Field(i).Name)
		}
	case reflect.Pointer:
		value.Set(reflect.New(value.Type().Elem()))
		populateResponsesCloneFixture(t, value.Elem(), path)
	case reflect.Slice:
		if value.Type() != reflect.TypeFor[json.RawMessage]() {
			t.Fatalf("add clone coverage for %s of type %s", path, value.Type())
		}
		value.SetBytes([]byte(` {"unknown":{"integer":9007199254740993,"decimal":1.234567890123456789e+123,"escaped":"\u4e2d","items":[null,false,0]}} `))
	case reflect.String:
		value.SetString(path + "-流式")
	case reflect.Bool:
		value.SetBool(true)
	case reflect.Uint:
		value.SetUint(uint64(^uint(0)))
	case reflect.Int:
		value.SetInt(int64(-int(^uint(0)>>1) - 1))
	case reflect.Float64:
		value.SetFloat(math.Float64frombits(0x3fd5555555555555))
	default:
		t.Fatalf("add clone coverage for %s of type %s", path, value.Type())
	}
}

func responsesCloneFixture(t testing.TB) *OpenAIResponsesRequest {
	t.Helper()
	request := &OpenAIResponsesRequest{}
	populateResponsesCloneFixture(t, reflect.ValueOf(request).Elem(), "request")
	return request
}

func assertResponsesCloneStorage(t *testing.T, original, cloned reflect.Value, path string) {
	t.Helper()
	switch original.Kind() {
	case reflect.Struct:
		for i := 0; i < original.NumField(); i++ {
			assertResponsesCloneStorage(t, original.Field(i), cloned.Field(i), path+"."+original.Type().Field(i).Name)
		}
	case reflect.Pointer:
		if original.IsNil() != cloned.IsNil() {
			t.Fatalf("%s changed nil state", path)
		}
		if !original.IsNil() {
			if original.Pointer() == cloned.Pointer() {
				t.Errorf("%s shares pointer storage", path)
			}
			assertResponsesCloneStorage(t, original.Elem(), cloned.Elem(), path)
		}
	case reflect.Slice:
		if original.IsNil() != cloned.IsNil() {
			t.Fatalf("%s changed nil state", path)
		}
		if original.Len() > 0 && original.Pointer() == cloned.Pointer() {
			t.Errorf("%s shares raw JSON storage", path)
		}
	}
}

func mutateResponsesCloneFixture(t *testing.T, value reflect.Value) {
	t.Helper()
	switch value.Kind() {
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			mutateResponsesCloneFixture(t, value.Field(i))
		}
	case reflect.Pointer:
		if !value.IsNil() {
			mutateResponsesCloneFixture(t, value.Elem())
		}
	case reflect.Slice:
		if value.Len() > 0 {
			value.Index(0).SetUint('!')
		}
	case reflect.String:
		value.SetString(value.String() + "-changed")
	case reflect.Bool:
		value.SetBool(!value.Bool())
	case reflect.Uint:
		value.SetUint(value.Uint() - 1)
	case reflect.Int:
		value.SetInt(value.Int() + 1)
	case reflect.Float64:
		value.SetFloat(value.Float() + 1)
	default:
		t.Fatalf("unhandled mutation type %s", value.Type())
	}
}

func TestOpenAIResponsesRequestCloneAllFieldsIndependent(t *testing.T) {
	original := responsesCloneFixture(t)
	cloned := original.Clone()
	if !reflect.DeepEqual(original, cloned) {
		t.Fatal("clone changed request values")
	}
	assertResponsesCloneStorage(t, reflect.ValueOf(original), reflect.ValueOf(cloned), "request")
	mutateResponsesCloneFixture(t, reflect.ValueOf(cloned).Elem())
	if !reflect.DeepEqual(original, responsesCloneFixture(t)) {
		t.Fatal("mutating clone changed the source")
	}
	wantClone := responsesCloneFixture(t)
	mutateResponsesCloneFixture(t, reflect.ValueOf(wantClone).Elem())
	mutateResponsesCloneFixture(t, reflect.ValueOf(original).Elem())
	if !reflect.DeepEqual(cloned, wantClone) {
		t.Fatal("mutating source changed the clone")
	}
}

func TestOpenAIResponsesRequestCloneNilEmptyAndRawJSON(t *testing.T) {
	var nilRequest *OpenAIResponsesRequest
	if nilRequest.Clone() != nil {
		t.Fatal("nil request became nonnil")
	}
	zero := &OpenAIResponsesRequest{}
	if !reflect.DeepEqual(zero, zero.Clone()) {
		t.Fatal("zero values changed")
	}
	for _, raw := range []json.RawMessage{nil, {}, []byte(`null`), []byte(`false`), []byte(`0`), []byte(`""`), []byte(`[]`), []byte(` {"future":{"big":9007199254740993,"number":1.2300e+123,"escaped":"\u4e2d"}} `)} {
		request := responsesCloneFixture(t)
		value := reflect.ValueOf(request).Elem()
		for i := 0; i < value.NumField(); i++ {
			field := value.Field(i)
			if field.Type() == reflect.TypeFor[json.RawMessage]() {
				field.SetBytes(bytes.Clone(raw))
			} else if field.Kind() == reflect.Pointer {
				// Non-nil zero pointers must remain present, including false/0.
				field.Set(reflect.New(field.Type().Elem()))
			}
		}
		cloned := request.Clone()
		if !reflect.DeepEqual(request, cloned) {
			t.Fatalf("nil/empty/explicit value changed for raw %q", raw)
		}
		assertResponsesCloneStorage(t, reflect.ValueOf(request), reflect.ValueOf(cloned), "request")
	}
}

func TestOpenAIResponsesRequestClonePreservesFloatBits(t *testing.T) {
	for _, bits := range []uint64{0, 1 << 63, 1, 0x7fefffffffffffff, 0x7ff0000000000000, 0xfff0000000000000, 0x7ff8000000000123} {
		request := &OpenAIResponsesRequest{}
		value := reflect.ValueOf(request).Elem()
		for i := 0; i < value.NumField(); i++ {
			field := value.Field(i)
			if field.Type() == reflect.TypeFor[*float64]() {
				field.Set(reflect.New(field.Type().Elem()))
				field.Elem().SetFloat(math.Float64frombits(bits))
			}
		}
		cloned := reflect.ValueOf(request.Clone()).Elem()
		for i := 0; i < value.NumField(); i++ {
			if value.Field(i).Type() == reflect.TypeFor[*float64]() && math.Float64bits(cloned.Field(i).Elem().Float()) != bits {
				t.Fatalf("%s changed numeric bits %x", value.Type().Field(i).Name, bits)
			}
		}
	}
}

var responsesCloneBenchmarkSink *OpenAIResponsesRequest

func BenchmarkOpenAIResponsesRequestClone160KInput(b *testing.B) {
	const tokens = 160000
	enc := codec.NewCl100kBase()
	pattern := "# Request processing\n```go\nfunc serve(ctx context.Context) error { return streamResponse(ctx) }\n```\nOrdered output requires complete text and an atomic settlement.\n"
	_, pieces, err := enc.Encode(strings.Repeat(pattern, tokens/20+2))
	if err != nil || len(pieces) < tokens {
		b.Fatalf("construct token fixture: pieces=%d error=%v", len(pieces), err)
	}
	prompt := strings.Join(pieces[:tokens], "")
	count, err := enc.Count(prompt)
	if err != nil || count != tokens {
		b.Fatalf("fixture token count=%d error=%v", count, err)
	}
	input, err := json.Marshal([]any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": prompt}}}})
	if err != nil {
		b.Fatal(err)
	}
	stream, maxOutput := true, uint(8192)
	request := &OpenAIResponsesRequest{
		Model: "gpt-4.1", Input: input, Stream: &stream, MaxOutputTokens: &maxOutput,
		Store: json.RawMessage(`false`), StreamOptions: &StreamOptions{IncludeUsage: true},
		Metadata:  json.RawMessage(`{"future":{"large_integer":9007199254740993}}`),
		Reasoning: &Reasoning{Effort: "high", Summary: "auto"},
	}
	b.Logf("verified input_tokens=%d prompt_bytes=%d raw_input_bytes=%d", count, len(prompt), len(input))
	b.Run("copier", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(input)))
		for b.Loop() {
			cloned, err := platformcopy.DeepCopy(request)
			if err != nil {
				b.Fatal(err)
			}
			responsesCloneBenchmarkSink = cloned
		}
	})
	b.Run("typed", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(input)))
		for b.Loop() {
			responsesCloneBenchmarkSink = request.Clone()
		}
	})
}
