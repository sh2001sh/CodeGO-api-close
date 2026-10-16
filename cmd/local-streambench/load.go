package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sh2001sh/new-api/constant"
	"github.com/shirou/gopsutil/process"
)

type percentile struct{ P50, P95, P99 float64 }
type processMetrics struct {
	PID                                       int
	Samples                                   int
	RSSMiBPeak, CPUCoresAverage, CPUCoresPeak float64
	Error                                     string `json:",omitempty"`
}
type loadResult struct {
	Config                                                                                                              benchConfig
	Tokenizer                                                                                                           string
	VerifiedPromptContentTokens, PromptBytes, WireRequestBytes, ExpectedTextBytes, ExpectedOutputTokens, ExpectedFrames int
	Requests, Successes, Failures                                                                                       int
	ElapsedSeconds, SuccessfulRequestsPerSecond                                                                         float64
	FirstRawFrameMS, FirstSemanticTextMS, CompleteMS, MaxTextGapMS                                                      percentile
	Process                                                                                                             processMetrics
	Observations                                                                                                        []observation `json:"observations"`
	MockStatsBefore, MockStatsAfter                                                                                     json.RawMessage
	Limitations                                                                                                         []string
}

func loopbackDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("refusing non-loopback dial")
	}
	return (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, network, address)
}
func localHTTPClient(timeout time.Duration, c int) *http.Client {
	return &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("redirects disabled") }, Transport: &http.Transport{Proxy: nil, DialContext: loopbackDial, MaxIdleConns: c*2 + 8, MaxIdleConnsPerHost: c + 2, MaxConnsPerHost: c + 2, IdleConnTimeout: 30 * time.Second, DisableCompression: true}}
}

func runLoad(c benchConfig) error {
	if err := c.validate(); err != nil {
		return err
	}
	w, err := makeWorkload(c)
	if err != nil {
		return err
	}
	client := localHTTPClient(c.Timeout, c.Concurrency)
	defer client.CloseIdleConnections()
	result := loadResult{Config: c, Tokenizer: "cl100k_base reference content tokenizer; not a claim about Claude/Gemini vendor tokenization", VerifiedPromptContentTokens: w.InputTokens, PromptBytes: w.PromptBytes, WireRequestBytes: len(w.Body), ExpectedTextBytes: len(w.Text), ExpectedOutputTokens: w.OutputTokens, ExpectedFrames: w.Frames, Requests: c.Requests, Limitations: []string{"Synthetic loopback upstream; no external provider capacity measured", "Percentiles include completed successful streams only; every failed request remains in observations", "Client stages use monotonic elapsed time; upstream stage durations use a separate monotonic clock and are not subtracted across processes", "Full request content is counted; transport envelopes add bytes and provider-specific message accounting can differ", "No warmup or automatic retries; -n is the fixed total attempted requests"}}
	result.MockStatsBefore = fetchMockStats()
	stopSampling := sampleProcess(c.PID, &result.Process)
	start := time.Now()
	var next atomic.Int64
	observations := make(chan observation, c.Requests)
	var wg sync.WaitGroup
	for worker := 0; worker < c.Concurrency; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			if c.Concurrency > 1 && c.Ramp > 0 {
				if !waitContext(context.Background(), time.Duration(int64(c.Ramp)*int64(worker)/int64(c.Concurrency-1))) {
					return
				}
			}
			for {
				index := int(next.Add(1))
				if index > c.Requests {
					return
				}
				observations <- requestOnce(client, c, w, index)
			}
		}(worker)
	}
	wg.Wait()
	close(observations)
	result.ElapsedSeconds = time.Since(start).Seconds()
	stopSampling()
	result.MockStatsAfter = fetchMockStats()
	var first, raw, complete, gap []float64
	for o := range observations {
		result.Observations = append(result.Observations, o)
		if o.OK {
			result.Successes++
			first = append(first, o.FirstTextMS)
			raw = append(raw, o.FirstRawMS)
			complete = append(complete, o.CompleteMS)
			gap = append(gap, o.MaxGapMS)
		} else {
			result.Failures++
		}
	}
	sort.Slice(result.Observations, func(i, j int) bool { return result.Observations[i].Index < result.Observations[j].Index })
	result.FirstSemanticTextMS = percentiles(first)
	result.FirstRawFrameMS = percentiles(raw)
	result.CompleteMS = percentiles(complete)
	result.MaxTextGapMS = percentiles(gap)
	result.SuccessfulRequestsPerSecond = float64(result.Successes) / result.ElapsedSeconds
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if c.Out != "" {
		if err = os.MkdirAll(filepath.Dir(c.Out), 0755); err != nil {
			return err
		}
		if err = os.WriteFile(c.Out, append(data, '\n'), 0600); err != nil {
			return err
		}
	}
	fmt.Println(string(data))
	if result.Failures > 0 {
		return fmt.Errorf("%d/%d streams failed strict verification", result.Failures, c.Requests)
	}
	return nil
}

func requestOnce(client *http.Client, c benchConfig, w workload, index int) (o observation) {
	o.Index = index
	start := time.Now()
	state := &streamState{o: &o, start: start}
	defer func() {
		o.CompleteMS = elapsed(start)
		o.TextBytes = state.text.Len()
		o.ThoughtBytes = state.thought.Len()
		o.ToolBytes = state.tool.Len()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	headers := http.Header{"Content-Type": []string{"application/json"}, "Authorization": []string{"Bearer sk-" + fixtureKey((index-1)%c.Users+1, c.Route)}, "X-Streambench-Request-Id": []string{fmt.Sprintf("streambench-%d", index)}}
	if c.Protocol == "claude" {
		headers.Set("anthropic-version", "2023-06-01")
	}
	if c.Protocol == "ws" {
		u, _ := url.Parse(strings.TrimRight(c.URL, "/") + w.Path)
		u.Scheme = "ws"
		headers.Set("OpenAI-Beta", "responses_websockets=2026-02-06")
		dialer := websocket.Dialer{NetDialContext: loopbackDial, HandshakeTimeout: 20 * time.Second, Proxy: nil, EnableCompression: false}
		conn, response, err := dialer.DialContext(ctx, u.String(), headers)
		if response != nil {
			o.Status = response.StatusCode
			o.RequestID = response.Header.Get(constant.RequestIdKey)
		}
		if err != nil {
			o.Error = err.Error()
			return
		}
		defer conn.Close()
		conn.SetReadLimit(16 << 20)
		_ = conn.SetReadDeadline(time.Now().Add(c.Timeout))
		_ = conn.SetWriteDeadline(time.Now().Add(20 * time.Second))
		if err = conn.WriteMessage(websocket.TextMessage, w.Body); err != nil {
			o.Error = err.Error()
			return
		}
		for !state.done {
			_, data, e := conn.ReadMessage()
			if e != nil {
				o.Error = e.Error()
				return
			}
			if e = state.frame(data, "ws"); e != nil {
				o.Error = e.Error()
				return
			}
		}
	} else {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.URL, "/")+w.Path, bytes.NewReader(w.Body))
		if err != nil {
			o.Error = err.Error()
			return
		}
		req.Header = headers
		response, err := client.Do(req)
		if err != nil {
			o.Error = err.Error()
			return
		}
		defer response.Body.Close()
		o.Status = response.StatusCode
		o.RequestID = response.Header.Get(constant.RequestIdKey)
		if response.StatusCode != 200 {
			body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
			o.Error = fmt.Sprintf("HTTP %d: %s", response.StatusCode, string(body))
			return
		}
		if !strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
			o.Error = "HTTP 200 without SSE content-type"
			return
		}
		if err = readSSE(response.Body, func(data []byte) error { return state.frame(data, c.Protocol) }); err != nil {
			o.Error = err.Error()
			return
		}
	}
	if err := state.finish(w); err != nil {
		o.Error = err.Error()
		return
	}
	o.OK = true
	return
}

func percentiles(values []float64) percentile {
	if len(values) == 0 {
		return percentile{}
	}
	sort.Float64s(values)
	p := func(q float64) float64 {
		return values[max(0, min(len(values)-1, int(math.Ceil(q*float64(len(values))))-1))]
	}
	return percentile{p(.5), p(.95), p(.99)}
}
func fetchMockStats() json.RawMessage {
	c := localHTTPClient(5*time.Second, 1)
	defer c.CloseIdleConnections()
	r, err := c.Get("http://127.0.0.1:18181/stats")
	if err != nil {
		return nil
	}
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil || !json.Valid(body) {
		return nil
	}
	return body
}

func sampleProcess(pid int, result *processMetrics) func() {
	result.PID = pid
	if pid == 0 {
		return func() {}
	}
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		result.Error = err.Error()
		return func() {}
	}
	cpu, err := p.Times()
	if err != nil {
		result.Error = err.Error()
		return func() {}
	}
	initial := cpu.Total()
	start := time.Now()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		last, lastAt := initial, start
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		sample := func() {
			now := time.Now()
			if mem, e := p.MemoryInfo(); e == nil {
				result.RSSMiBPeak = max(result.RSSMiBPeak, float64(mem.RSS)/(1<<20))
			} else {
				result.Error = e.Error()
			}
			if t, e := p.Times(); e == nil {
				if seconds := now.Sub(lastAt).Seconds(); seconds > 0 {
					result.CPUCoresPeak = max(result.CPUCoresPeak, finiteCPURatio(t.Total()-last, seconds))
					last, lastAt = t.Total(), now
				}
				if seconds := now.Sub(start).Seconds(); seconds > 0 {
					result.CPUCoresAverage = finiteCPURatio(t.Total()-initial, seconds)
				}
			} else {
				result.Error = e.Error()
			}
			result.Samples++
		}
		sample()
		for {
			select {
			case <-stop:
				sample()
				return
			case <-ticker.C:
				sample()
			}
		}
	}()
	return func() { close(stop); <-done }
}

func finiteCPURatio(cpuSeconds, elapsedSeconds float64) float64 {
	if elapsedSeconds <= 0 || cpuSeconds < 0 || math.IsNaN(cpuSeconds) || math.IsNaN(elapsedSeconds) || math.IsInf(cpuSeconds, 0) || math.IsInf(elapsedSeconds, 0) {
		return 0
	}
	ratio := cpuSeconds / elapsedSeconds
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		return 0
	}
	return ratio
}
