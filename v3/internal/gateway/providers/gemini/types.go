package gemini

import "encoding/json"

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type part struct {
	Text             string            `json:"text,omitempty"`
	Thought          bool              `json:"thought,omitempty"`
	ThoughtSignature string            `json:"thoughtSignature,omitempty"`
	InlineData       *inlineData       `json:"inlineData,omitempty"`
	FileData         *fileData         `json:"fileData,omitempty"`
	FunctionCall     *functionCall     `json:"functionCall,omitempty"`
	FunctionResponse *functionResponse `json:"functionResponse,omitempty"`
	ExecutableCode   json.RawMessage   `json:"executableCode,omitempty"`
	CodeResult       json.RawMessage   `json:"codeExecutionResult,omitempty"`
}

type inlineData struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

type fileData struct {
	MIMEType string `json:"mimeType"`
	URI      string `json:"fileUri"`
}

type functionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type functionResponse struct {
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

type functionDeclaration struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parametersJsonSchema,omitempty"`
}

type tool struct {
	Functions []functionDeclaration `json:"functionDeclarations"`
}

type toolConfig struct {
	Calling functionCallingConfig `json:"functionCallingConfig"`
}

type functionCallingConfig struct {
	Mode    string   `json:"mode"`
	Allowed []string `json:"allowedFunctionNames,omitempty"`
}

type generationConfig struct {
	Temperature      *float64        `json:"temperature,omitempty"`
	TopP             *float64        `json:"topP,omitempty"`
	MaxTokens        *int64          `json:"maxOutputTokens,omitempty"`
	Candidates       *int64          `json:"candidateCount,omitempty"`
	Stop             []string        `json:"stopSequences,omitempty"`
	MIMEType         string          `json:"responseMimeType,omitempty"`
	Schema           json.RawMessage `json:"responseJsonSchema,omitempty"`
	Seed             *int64          `json:"seed,omitempty"`
	PresencePenalty  *float64        `json:"presencePenalty,omitempty"`
	FrequencyPenalty *float64        `json:"frequencyPenalty,omitempty"`
	Thinking         *thinkingConfig `json:"thinkingConfig,omitempty"`
}

type thinkingConfig struct {
	Budget  int64 `json:"thinkingBudget"`
	Include bool  `json:"includeThoughts"`
}

type generateRequest struct {
	Contents []content        `json:"contents"`
	System   *content         `json:"systemInstruction,omitempty"`
	Config   generationConfig `json:"generationConfig,omitempty"`
	Tools    []tool           `json:"tools,omitempty"`
	Tool     *toolConfig      `json:"toolConfig,omitempty"`
}

type candidate struct {
	Index        int     `json:"index"`
	Content      content `json:"content"`
	FinishReason string  `json:"finishReason"`
}

type usageMetadata struct {
	Prompt        int64            `json:"promptTokenCount"`
	Output        int64            `json:"candidatesTokenCount"`
	Cached        int64            `json:"cachedContentTokenCount"`
	Thoughts      int64            `json:"thoughtsTokenCount"`
	ToolInput     int64            `json:"toolUsePromptTokenCount"`
	PromptDetails []modalityTokens `json:"promptTokensDetails"`
	OutputDetails []modalityTokens `json:"candidatesTokensDetails"`
}

type modalityTokens struct {
	Modality string `json:"modality"`
	Count    int64  `json:"tokenCount"`
}

type generateResponse struct {
	ID         string         `json:"responseId"`
	Candidates []candidate    `json:"candidates"`
	Usage      *usageMetadata `json:"usageMetadata"`
	Error      *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
	Feedback *struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
}
