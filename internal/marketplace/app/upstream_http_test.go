package app

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
	"github.com/stretchr/testify/require"
)

type marketplaceUnexpectedTransport struct{}

func (marketplaceUnexpectedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("uncontrolled default HTTP transport is disabled in isolated marketplace tests")
}

// Keep integration fixtures on loopback while exercising the real HTTPS
// transport, public-IP policy, provider headers, TLS validation and redirect rule.
// Only the checked numeric dial target is mapped to this isolated test listener.
func marketplaceTestUpstream(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	client := platformhttpx.NewMarketplaceHTTPClient(
		func(_ context.Context, host string) ([]net.IPAddr, error) {
			if host != "example.com" {
				return nil, fmt.Errorf("unexpected test hostname")
			}
			return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
		},
		func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != "93.184.216.34:443" {
				return nil, fmt.Errorf("unchecked test dial target")
			}
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		},
	)
	transport := client.Transport.(*platformhttpx.MarketplaceTransport)
	transport.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	require.False(t, transport.TLSClientConfig.InsecureSkipVerify)
	previous := marketplaceUpstreamHTTPClient
	previousDefault := http.DefaultTransport
	// A regression back to an ad-hoc default client must fail locally instead
	// of contacting the public hostname used by this fixture.
	http.DefaultTransport = marketplaceUnexpectedTransport{}
	marketplaceUpstreamHTTPClient = client
	t.Cleanup(func() {
		client.CloseIdleConnections()
		marketplaceUpstreamHTTPClient = previous
		http.DefaultTransport = previousDefault
		server.Close()
	})
	server.URL = "https://example.com"
	return server
}

func TestMarketplaceUpstreamRealCallsRejectAllRedirectsWithoutLeakingCredentials(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		for _, provider := range []string{"openai_compatible", "codex", "anthropic", "azure_openai", "gemini"} {
			t.Run(fmt.Sprintf("%d/%s", status, provider), func(t *testing.T) {
				var initial, redirected atomic.Int64
				server := marketplaceTestUpstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/sink" {
						redirected.Add(1)
						return
					}
					initial.Add(1)
					key := "Authorization"
					want := "Bearer isolated-test-key"
					switch provider {
					case "anthropic":
						key, want = "x-api-key", "isolated-test-key"
					case "azure_openai":
						key, want = "api-key", "isolated-test-key"
					case "gemini":
						key, want = "x-goog-api-key", "isolated-test-key"
					}
					require.Equal(t, want, r.Header.Get(key))
					if r.Method == http.MethodPost {
						body, err := io.ReadAll(r.Body)
						require.NoError(t, err)
						if provider == "gemini" {
							require.Contains(t, r.URL.Path, "gpt-5")
							require.Contains(t, string(body), "Reply with OK.")
						} else {
							require.Contains(t, string(body), "gpt-5")
						}
					}
					w.Header().Set("Location", "https://example.com/sink?secret=redirect-target")
					w.WriteHeader(status)
					_, _ = w.Write([]byte("PRIVATE-RESPONSE-CANARY"))
				}))
				_, err := fetchUpstreamModelsContext(context.Background(), provider, server.URL, "isolated-test-key")
				require.ErrorContains(t, err, fmt.Sprintf("HTTP %d", status))
				require.NotContains(t, err.Error(), "CANARY")
				require.NotContains(t, err.Error(), "redirect-target")
				_, err = probeMarketplaceInferenceTimedContext(context.Background(), provider, server.URL, "isolated-test-key", "gpt-5")
				require.ErrorContains(t, err, fmt.Sprintf("HTTP %d", status))
				require.NotContains(t, err.Error(), "CANARY")
				require.EqualValues(t, 2, initial.Load(), "one real GET and one real POST, with no retries")
				require.Zero(t, redirected.Load(), "even same-host redirects must never be fetched")
			})
		}
	}
}

func TestMarketplaceUpstreamCrossHostRedirectDoesNotSendCredentialsOrBody(t *testing.T) {
	var sinkRequests atomic.Int64
	sink := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sinkRequests.Add(1) }))
	defer sink.Close()
	server := marketplaceTestUpstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", sink.URL+"/credential-sink")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	for _, provider := range []string{"openai_compatible", "anthropic", "azure_openai", "gemini"} {
		_, err := probeMarketplaceInferenceTimedContext(context.Background(), provider, server.URL, "secret-canary", "gpt-5")
		require.ErrorContains(t, err, "HTTP 307")
	}
	require.Zero(t, sinkRequests.Load())
}

func TestMarketplaceUpstreamBlocksUnsafeTargetsBeforeDial(t *testing.T) {
	var dials atomic.Int64
	client := platformhttpx.NewMarketplaceHTTPClient(
		func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		},
		func(context.Context, string, string) (net.Conn, error) {
			dials.Add(1)
			return nil, fmt.Errorf("must not dial")
		},
	)
	defer client.CloseIdleConnections()
	for _, address := range []string{
		"ftp://8.8.8.8", "http://user:password@8.8.8.8", "http://8.8.8.8:0", "http://8.8.8.8:65536",
		"http://127.0.0.1:8080", "http://10.0.0.1:65535", "http://private.example",
		"https://user:password@8.8.8.8", "https://8.8.8.8:0", "https://8.8.8.8:65536",
		"https://127.0.0.1:8080", "https://10.0.0.1:65535",
		"https://127.0.0.1", "https://10.0.0.1", "https://172.16.1.2", "https://192.168.1.2", "https://100.64.0.1",
		"https://169.254.169.254", "https://0.0.0.0", "https://192.0.2.1", "https://198.18.0.1", "https://224.0.0.1",
		"https://[::1]", "https://[::]", "https://[fc00::1]", "https://[fe80::1]", "https://[ff02::1]",
		"https://[::ffff:127.0.0.1]", "https://[2001:db8::1]", "https://[64:ff9b::7f00:1]", "https://private.example",
		"https://[fec0::1]", "https://[2002:7f00:1::1]", "https://[4000::1]",
	} {
		t.Run(address, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodGet, address, nil)
			require.NoError(t, err)
			_, err = client.Do(request)
			require.Error(t, err)
		})
	}
	require.Zero(t, dials.Load())
}

func TestMarketplaceUpstreamRejectsMixedDNSAnswersBeforeAnyDial(t *testing.T) {
	for _, unsafeIP := range []string{"127.0.0.1", "10.2.3.4", "::1", "fd00::1"} {
		dials := 0
		client := platformhttpx.NewMarketplaceHTTPClient(
			func(context.Context, string) ([]net.IPAddr, error) {
				return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP(unsafeIP)}}, nil
			},
			func(context.Context, string, string) (net.Conn, error) {
				dials++
				return nil, fmt.Errorf("must not dial")
			},
		)
		request, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
		require.NoError(t, err)
		_, err = client.Do(request)
		require.ErrorIs(t, err, platformhttpx.ErrMarketplaceUpstreamAddress)
		require.Zero(t, dials)
		client.CloseIdleConnections()
	}
}

func TestMarketplaceUpstreamPinsValidatedDNSAndRejectsRebinding(t *testing.T) {
	var lookups, dials, served atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served.Add(1)
		w.Header().Set("Connection", "close")
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()
	client := platformhttpx.NewMarketplaceHTTPClient(
		func(context.Context, string) ([]net.IPAddr, error) {
			if lookups.Add(1) > 1 {
				return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
			}
			return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
		},
		func(ctx context.Context, network, address string) (net.Conn, error) {
			dials.Add(1)
			require.Equal(t, "93.184.216.34:443", address, "dial must receive the checked literal, never the hostname")
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		},
	)
	defer client.CloseIdleConnections()
	client.Transport.(*platformhttpx.MarketplaceTransport).TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	response, err := client.Get("https://example.com")
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	_, err = client.Get("https://example.com")
	require.ErrorIs(t, err, platformhttpx.ErrMarketplaceUpstreamAddress)
	require.EqualValues(t, 2, lookups.Load())
	require.EqualValues(t, 1, dials.Load())
	require.EqualValues(t, 1, served.Load())
}

func TestMarketplaceUpstreamTLSCannotUseGlobalInsecureSetting(t *testing.T) {
	previous := platformconfig.TLSInsecureSkipVerify
	platformconfig.TLSInsecureSkipVerify = true
	defer func() { platformconfig.TLSInsecureSkipVerify = previous }()
	var served atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { served.Add(1) }))
	defer server.Close()
	for _, trustedRoot := range []bool{false, true} {
		client := platformhttpx.NewMarketplaceHTTPClient(
			func(context.Context, string) ([]net.IPAddr, error) {
				return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
			},
			func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			},
		)
		if trustedRoot {
			client.Transport.(*platformhttpx.MarketplaceTransport).TLSClientConfig = &tls.Config{RootCAs: server.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs}
		}
		// With a trusted root, the wrong hostname still has to fail.
		_, err := client.Get("https://wrong-host.example")
		require.Error(t, err)
		client.CloseIdleConnections()
	}
	require.Zero(t, served.Load(), "untrusted or wrong-host TLS must fail before HTTP credentials/body")
}

func TestMarketplaceUpstreamNeverReturnsUntrustedBodyInErrors(t *testing.T) {
	for _, body := range []string{
		`{"error":{"message":"PRIVATE-RESPONSE-CANARY"}}`,
		`{"success":false,"message":"PRIVATE-RESPONSE-CANARY"}`,
		`{"choices":[{"message":{"content":"error: PRIVATE-RESPONSE-CANARY"}}]}`,
		"error: PRIVATE-RESPONSE-CANARY",
	} {
		server := marketplaceTestUpstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		_, err := probeMarketplaceInferenceTimedContext(context.Background(), "openai_compatible", server.URL, "isolated-test-key", "gpt-5")
		require.Error(t, err)
		require.NotContains(t, err.Error(), "CANARY")
	}
	server := marketplaceTestUpstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = io.WriteString(w, strings.Repeat("PRIVATE-RESPONSE-CANARY", 100))
	}))
	_, err := fetchUpstreamModelsContext(context.Background(), "openai_compatible", server.URL, "isolated-test-key")
	require.ErrorContains(t, err, "HTTP 400")
	require.NotContains(t, err.Error(), "CANARY")
}

func TestMarketplaceUpstreamDialsPublicCustomPortsLiterally(t *testing.T) {
	for _, address := range []string{"8.8.8.8:8443", "178.83.203.233:8080", "8.8.8.8:65535", "[2001:4860:4860::8888]:8080"} {
		dials := 0
		client := platformhttpx.NewMarketplaceHTTPClient(
			func(context.Context, string) ([]net.IPAddr, error) {
				t.Fatal("literal IP must not use DNS")
				return nil, nil
			},
			func(_ context.Context, _, target string) (net.Conn, error) {
				dials++
				require.Equal(t, address, target)
				return nil, fmt.Errorf("isolated test does not connect to public targets")
			},
		)
		_, err := client.Get("https://" + address)
		require.Error(t, err)
		require.Equal(t, 1, dials)
		client.CloseIdleConnections()
	}
}

func TestMarketplaceUpstreamIgnoresProxyEnvironment(t *testing.T) {
	var proxyCalls atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { proxyCalls.Add(1); w.WriteHeader(500) }))
	defer proxy.Close()
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("HTTP_PROXY", proxy.URL)
	server := marketplaceTestUpstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"isolated-model"}]}`)
	}))
	models, err := fetchUpstreamModelsContext(context.Background(), "openai_compatible", server.URL, "isolated-test-key")
	require.NoError(t, err)
	require.Equal(t, []string{"isolated-model"}, models)
	require.Zero(t, proxyCalls.Load())
}

func TestMarketplaceHTTPModelDiscoveryAndInference(t *testing.T) {
	var served atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served.Add(1)
		require.Equal(t, "example.com:8080", r.Host)
		require.Equal(t, "Bearer isolated-test-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			require.Equal(t, http.MethodGet, r.Method)
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-5"}]}`)
		case "/v1/chat/completions":
			require.Equal(t, http.MethodPost, r.Method)
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"OK"}}]}`)
		default:
			t.Errorf("unexpected upstream path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := platformhttpx.NewMarketplaceHTTPClient(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}, func(ctx context.Context, network, address string) (net.Conn, error) {
		require.Equal(t, "93.184.216.34:8080", address)
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	})
	defer client.CloseIdleConnections()
	previous := marketplaceUpstreamHTTPClient
	marketplaceUpstreamHTTPClient = client
	t.Cleanup(func() { marketplaceUpstreamHTTPClient = previous })
	models, err := fetchUpstreamModelsContext(context.Background(), "openai_compatible", "http://example.com:8080/v1", "isolated-test-key")
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5"}, models)
	_, err = probeMarketplaceInferenceTimedContext(context.Background(), "openai_compatible", "http://example.com:8080/v1", "isolated-test-key", "gpt-5")
	require.NoError(t, err)
	require.EqualValues(t, 2, served.Load())
}

func TestMarketplaceUpstreamRequestErrorsDoNotExposeDNSOrURLSecrets(t *testing.T) {
	client := platformhttpx.NewMarketplaceHTTPClient(
		func(context.Context, string) ([]net.IPAddr, error) { return nil, fmt.Errorf("PRIVATE-DNS-CANARY") },
		func(context.Context, string, string) (net.Conn, error) {
			t.Fatal("DNS failure must not dial")
			return nil, nil
		},
	)
	previous := marketplaceUpstreamHTTPClient
	marketplaceUpstreamHTTPClient = client
	t.Cleanup(func() { client.CloseIdleConnections(); marketplaceUpstreamHTTPClient = previous })
	_, err := fetchUpstreamModelsContext(context.Background(), "openai_compatible", "https://example.com?secret=PRIVATE-URL-CANARY", "isolated-test-key")
	require.EqualError(t, err, "上游请求失败")
	_, err = probeMarketplaceInferenceTimedContext(context.Background(), "openai_compatible", "https://example.com?secret=PRIVATE-URL-CANARY", "isolated-test-key", "gpt-5")
	require.EqualError(t, err, "上游请求失败")
}
