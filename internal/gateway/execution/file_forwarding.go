package execution

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	gatewayproviders "github.com/sh2001sh/new-api/internal/gateway/execution/providers"
	gatewayfiles "github.com/sh2001sh/new-api/internal/gateway/files"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/sh2001sh/new-api/internal/platform/logger"
)

const upstreamFileResponseLimit = 1 << 20

func prepareTextFileReferences(c *gin.Context, info *relaycommon.RelayInfo, adaptor gatewayproviders.SyncAdaptor, source *dto.GeneralOpenAIRequest) (*dto.GeneralOpenAIRequest, error) {
	if !hasResolvedFileReferences(c) {
		return source, nil
	}
	raw, err := prepareSelectedChannelFileJSON(c, info, adaptor, source, "openai_chat")
	if err != nil {
		return nil, err
	}
	prepared := &dto.GeneralOpenAIRequest{}
	if err := platformencoding.Unmarshal(raw, prepared); err != nil {
		return nil, err
	}
	return prepared, nil
}

func prepareResponsesFileReferences(c *gin.Context, info *relaycommon.RelayInfo, adaptor gatewayproviders.SyncAdaptor, source any) (any, error) {
	if !hasResolvedFileReferences(c) {
		return source, nil
	}
	raw, err := prepareSelectedChannelFileJSON(c, info, adaptor, source, "openai_responses")
	if err != nil {
		return nil, err
	}
	switch source.(type) {
	case *dto.OpenAIResponsesRequest:
		prepared := &dto.OpenAIResponsesRequest{}
		return prepared, platformencoding.Unmarshal(raw, prepared)
	case *dto.OpenAIResponsesCompactionRequest:
		prepared := &dto.OpenAIResponsesCompactionRequest{}
		return prepared, platformencoding.Unmarshal(raw, prepared)
	default:
		return nil, fmt.Errorf("unsupported file-bearing request type %T", source)
	}
}

func prepareSelectedChannelFileJSON(c *gin.Context, info *relaycommon.RelayInfo, adaptor gatewayproviders.SyncAdaptor, source any, protocol string) ([]byte, error) {
	raw, err := platformencoding.Marshal(source)
	if err != nil {
		return nil, err
	}
	options := gatewayfiles.PrepareOptions{
		UserID: info.UserId,
		Admin:  c.GetInt("role") >= constant.RoleAdminUser,
		Mode:   gatewayfiles.NormalizeInputMode(info.ChannelSetting.FileInputMode),
		SignedURL: func(file *gatewayschema.UserFile) (string, error) {
			return gatewayfiles.BuildSignedDeliveryURL(file.ID, info.AttemptStartTime)
		},
		OnFallback: func(mode gatewayfiles.InputMode, fallbackErr error) {
			logger.LogDebug(c, fmt.Sprintf("file input %s fallback for channel %d: %v", mode, info.ChannelId, fallbackErr))
		},
	}
	if supportsNativeFileUpload(info) {
		options.NativeUpload = func(file *gatewayschema.UserFile) (string, error) {
			return resolveNativeUpstreamFile(c, info, adaptor, file, protocol)
		}
	}
	return gatewayfiles.PrepareFileIDsJSON(raw, options)
}

func supportsNativeFileUpload(info *relaycommon.RelayInfo) bool {
	if info == nil || info.ChannelMeta == nil || info.ApiType != constant.APITypeOpenAI || info.ChannelType != constant.ChannelTypeOpenAI {
		return false
	}
	for name := range info.HeadersOverride {
		if strings.EqualFold(name, "authorization") {
			return false
		}
	}
	return strings.TrimSpace(info.ApiKey) != ""
}

func resolveNativeUpstreamFile(c *gin.Context, info *relaycommon.RelayInfo, adaptor gatewayproviders.SyncAdaptor, file *gatewayschema.UserFile, protocol string) (string, error) {
	target := gatewayfiles.UpstreamTarget{
		ChannelID:          info.ChannelId,
		KeyFingerprint:     gatewayfiles.FingerprintCredential(info.ApiKey),
		BaseURLFingerprint: gatewayfiles.FingerprintBaseURL(info.ChannelBaseUrl),
		Protocol:           protocol,
	}
	return gatewayfiles.ResolveUpstreamFile(file, target, func() (string, error) {
		return uploadNativeUpstreamFile(c, info, adaptor, file)
	})
}

func uploadNativeUpstreamFile(c *gin.Context, info *relaycommon.RelayInfo, adaptor gatewayproviders.SyncAdaptor, file *gatewayschema.UserFile) (string, error) {
	content, err := gatewayfiles.OpenContent(file)
	if err != nil {
		return "", err
	}
	defer content.Close()

	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	writeDone := make(chan error, 1)
	go streamMultipartFile(writer, multipartWriter, content, file, writeDone)

	target := relaycommon.GetFullRequestURL(info.ChannelBaseUrl, "/v1/files", info.ChannelType)
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, target, reader)
	if err != nil {
		_ = reader.Close()
		return "", err
	}
	headers := req.Header
	if err := adaptor.SetupRequestHeader(c, &headers, info); err != nil {
		_ = reader.Close()
		return "", err
	}
	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	req.Header.Set("Accept", "application/json")

	client, err := platformhttpx.GetHTTPClientWithProxy(info.ChannelSetting.Proxy)
	if err != nil {
		_ = reader.Close()
		return "", err
	}
	resp, requestErr := client.Do(req)
	if requestErr != nil {
		_ = reader.Close()
		<-writeDone
		return "", fmt.Errorf("upload file to upstream: %w", requestErr)
	}
	if resp == nil {
		_ = reader.Close()
		<-writeDone
		return "", fmt.Errorf("upstream files API returned no response")
	}
	writeErr := <-writeDone
	defer resp.Body.Close()
	if writeErr != nil {
		return "", fmt.Errorf("stream file to upstream: %w", writeErr)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, upstreamFileResponseLimit))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("upstream files API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var response struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("decode upstream file response: %w", err)
	}
	if strings.TrimSpace(response.ID) == "" {
		return "", fmt.Errorf("upstream files API returned an empty id")
	}
	return response.ID, nil
}

func streamMultipartFile(pipe *io.PipeWriter, writer *multipart.Writer, content io.Reader, file *gatewayschema.UserFile, done chan<- error) {
	var err error
	defer func() {
		if closeErr := writer.Close(); err == nil {
			err = closeErr
		}
		_ = pipe.CloseWithError(err)
		done <- err
	}()
	purpose := strings.TrimSpace(file.Purpose)
	if purpose == "" {
		purpose = "user_data"
	}
	if err = writer.WriteField("purpose", purpose); err != nil {
		return
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, strings.ReplaceAll(file.Filename, `"`, `'`)))
	header.Set("Content-Type", file.MimeType)
	var part io.Writer
	part, err = writer.CreatePart(header)
	if err != nil {
		return
	}
	_, err = io.Copy(part, content)
}
