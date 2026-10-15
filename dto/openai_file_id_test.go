package dto

import (
	"encoding/json"
	"testing"

	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestResponsesParseInputPreservesLocalFileIDs(t *testing.T) {
	request := OpenAIResponsesRequest{Input: json.RawMessage(`[{"role":"user","content":[{"type":"input_image","file_id":"file-codego-image"},{"type":"input_file","file_id":"file-codego-document"}]}]`)}
	inputs := request.ParseInput()
	require.Len(t, inputs, 2)
	require.Equal(t, "file-codego-image", inputs[0].FileID)
	require.Equal(t, "file-codego-document", inputs[1].FileID)
	meta := request.GetTokenCountMeta()
	require.Len(t, meta.Files, 2)
	_, imageOK := meta.Files[0].Source.(*types.FileIDSource)
	_, fileOK := meta.Files[1].Source.(*types.FileIDSource)
	require.True(t, imageOK)
	require.True(t, fileOK)
}

func TestChatFileContentCreatesLocalFileSource(t *testing.T) {
	content := MediaContent{Type: ContentTypeFile, File: &MessageFile{FileId: "file-codego-document"}}
	_, ok := content.ToFileSource().(*types.FileIDSource)
	require.True(t, ok)

	content.File = &MessageFile{FileId: "file-upstream-owned"}
	require.Nil(t, content.ToFileSource())
}
