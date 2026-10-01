package dify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// WithTransport keeps Dify's upload and Chat requests on the gateway's channel
// transport, including its proxy and TLS settings.
func (p Provider) WithTransport(transport http.RoundTripper) http.RoundTripper {
	client := http.Client{}
	if p.Client != nil {
		client = *p.Client
	}
	client.Transport = transport
	p.Client, p.injected = &client, true
	return p
}

// UpstreamTransport implements the gateway's native multi-request hook.
func (p Provider) UpstreamTransport(_ *gateway.Request, fallback http.RoundTripper) http.RoundTripper {
	return p.WithTransport(fallback)
}

// RoundTrip uploads inline images before submitting the native Chat request.
// BuildRequest remains network-free and stores binary images only in context.
func (p Provider) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.Body == nil || req.URL == nil {
		return nil, fmt.Errorf("dify: invalid transport request")
	}
	defer func() { _ = req.Body.Close() }()
	opts, ok := req.Context().Value(uploadKey{}).(uploadOptions)
	if !ok || req.GetBody == nil {
		return nil, fmt.Errorf("dify: transport requires a built request")
	}
	client, closeIdle, err := p.proxiedClient(opts)
	if err != nil {
		return nil, err
	}
	if closeIdle != nil {
		defer closeIdle()
	}
	if len(opts.images) == 0 {
		resp, err := client.Do(req)
		return rejectRedirect(resp), err
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	var native nativeRequest
	err = json.NewDecoder(body).Decode(&native)
	_ = body.Close()
	if err != nil {
		return nil, fmt.Errorf("dify: invalid prepared Chat body")
	}
	for _, image := range opts.images {
		if resp, err := uploadInlineImage(req, &client, &native, image); resp != nil || err != nil {
			return resp, err
		}
	}
	data, err := json.Marshal(native)
	if err != nil {
		return nil, err
	}
	out := req.Clone(req.Context())
	out.Body = io.NopCloser(bytes.NewReader(data))
	out.ContentLength = int64(len(data))
	out.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
	resp, err := client.Do(out)
	return rejectRedirect(resp), err
}

// proxiedClient builds the http.Client used for both the upload and Chat
// requests, cloning the base transport to apply the channel proxy only when
// the gateway hasn't already injected its own transport.
func (p Provider) proxiedClient(opts uploadOptions) (http.Client, func(), error) {
	client := http.Client{}
	if p.Client != nil {
		client = *p.Client
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if opts.proxy == nil || p.injected {
		return client, nil, nil
	}
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	base, ok := transport.(*http.Transport)
	if !ok {
		return http.Client{}, nil, fmt.Errorf("dify: custom transport cannot configure the channel proxy")
	}
	cloned := base.Clone()
	cloned.Proxy = http.ProxyURL(opts.proxy)
	client.Transport = cloned
	return client, cloned.CloseIdleConnections, nil
}

// uploadInlineImage uploads one buffered inline image to Dify's file upload
// endpoint and records the resulting upload_file_id on native.Files. A
// non-nil returned *http.Response signals that RoundTrip should return
// immediately (an upload failure response); nil, nil means continue.
func uploadInlineImage(req *http.Request, client *http.Client, native *nativeRequest, image inlineImage) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	if err := writer.WriteField("user", native.User); err != nil {
		return nil, err
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="file"; filename="image.`+strings.TrimPrefix(image.mime, "image/")+`"`)
	header.Set("Content-Type", image.mime)
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(image.data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	uploadURL := *req.URL
	uploadURL.Path = strings.TrimSuffix(req.URL.Path, "/chat-messages") + "/files/upload"
	uploadURL.RawPath = ""
	upload, err := http.NewRequestWithContext(req.Context(), http.MethodPost, uploadURL.String(), &buffer)
	if err != nil {
		return nil, err
	}
	upload.Header.Set("Authorization", req.Header.Get("Authorization"))
	upload.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := client.Do(upload)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return rejectRedirect(resp), nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10+1))
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	var result struct {
		ID string `json:"id"`
	}
	if len(data) > 64<<10 || json.Unmarshal(data, &result) != nil || strings.TrimSpace(result.ID) == "" {
		return nil, fmt.Errorf("dify: upload returned no valid file ID")
	}
	if image.index < 0 || image.index >= len(native.Files) {
		return nil, fmt.Errorf("dify: invalid prepared image index")
	}
	native.Files[image.index].UploadFileID = result.ID
	return nil, nil
}

// The caller may itself be an http.Client. Convert redirects to an explicit
// failure so that outer clients cannot redirect upload credentials or bodies.
func rejectRedirect(resp *http.Response) *http.Response {
	if resp == nil || resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return resp
	}
	_ = resp.Body.Close()
	const body = `{"code":"dify_redirect_rejected","message":"Dify upstream redirects are unsupported"}`
	resp.StatusCode, resp.Status = http.StatusBadGateway, "502 Bad Gateway"
	resp.Header = http.Header{"Content-Type": []string{"application/json"}}
	resp.Body = io.NopCloser(strings.NewReader(body))
	resp.ContentLength = int64(len(body))
	return resp
}
