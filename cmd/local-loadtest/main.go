// local-loadtest exercises the real gateway with disposable loopback fixtures.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/process"
)

const fixtureDSN = "postgres://loadtest@127.0.0.1:55437/codego_loadtest?sslmode=disable"

func main() {
	mode := flag.String("mode", "run", "seed, prepare-long, upstream, run, or check")
	base := flag.String("url", "http://127.0.0.1:18180", "loopback gateway URL")
	concurrency := flag.Int("c", 32, "concurrent clients")
	duration := flag.Duration("duration", 15*time.Second, "measurement duration; in-flight requests drain afterwards")
	ramp := flag.Duration("ramp", 0, "spread initial client starts over this duration; included in measurement")
	users := flag.Int("users", 128, "fixture users (1 tests a shared wallet)")
	stream := flag.Bool("stream", true, "use SSE")
	payload := flag.Int("payload", 1024, "prompt bytes")
	ttft := flag.Duration("ttft", 10*time.Millisecond, "mock upstream first-token delay")
	tail := flag.Duration("tail", 40*time.Millisecond, "mock upstream delay after first token")
	failEvery := flag.Int64("fail-every", 0, "mock upstream returns 503 every N requests")
	pid := flag.Int("pid", 0, "gateway PID for process CPU/RSS sampling")
	out := flag.String("out", "", "JSON result file")
	profile := profiledWorkload{}
	flag.BoolVar(&profile.SkipWarmup, "skip-warmup", false, "skip the unmeasured completion after a separate smoke test")
	flag.StringVar(&profile.Protocol, "protocol", "chat", "chat or responses")
	flag.StringVar(&profile.Model, "model", "gpt-4.1", "fixture model")
	flag.IntVar(&profile.InputTokens, "input-tokens", 0, "actual mixed-code prompt tokens; 0 retains legacy byte payload")
	flag.IntVar(&profile.OutputTokens, "output-tokens", 1500, "profiled stream output tokens")
	flag.IntVar(&profile.CachedTokens, "cached-tokens", 0, "simulated upstream cached input tokens")
	flag.IntVar(&profile.ChunkTokens, "chunk-tokens", 8, "output tokens per SSE content frame")
	flag.DurationVar(&profile.Timeout, "request-timeout", 90*time.Second, "client request timeout including stream")
	flag.Int64Var(&profile.DropEvery, "drop-every", 0, "truncate every Nth profiled upstream response halfway")
	flag.Parse()
	var err error
	switch *mode {
	case "seed":
		err = seedFixtures()
	case "check":
		err = checkFixtures()
	case "prepare-long":
		err = prepareLongFixtures()
	case "upstream":
		err = serveUpstream(*ttft, *tail, *failEvery, profile)
	case "run":
		err = runLoad(*base, *concurrency, *users, *duration, *ramp, *stream, *payload, *pid, *out, profile)
	default:
		err = fmt.Errorf("unknown mode %q", *mode)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func loopbackURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("only an explicit loopback HTTP address is allowed")
	}
	return nil
}

func fixtureKey(user int) string { return fmt.Sprintf("localloadtestfixture%048d", user) }

func serveUpstream(first, tail time.Duration, failEvery int64, profile profiledWorkload) error {
	if err := profile.validate(); err != nil {
		return err
	}
	var calls atomic.Int64
	var active, peak atomic.Int64
	return http.ListenAndServe("127.0.0.1:18181", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stats" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]int64{"calls": calls.Load(), "active": active.Load(), "peak": peak.Load()})
			return
		}
		var input struct{ Stream bool }
		if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&input); err != nil {
			http.Error(w, "invalid fixture input", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()
		n := calls.Add(1)
		current := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); current > old; old = peak.Load() {
			if peak.CompareAndSwap(old, current) {
				break
			}
		}
		if failEvery > 0 && n%failEvery == 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(503)
			_, _ = io.WriteString(w, `{"error":{"message":"fixture overloaded","type":"server_error"}}`)
			return
		}
		if profile.InputTokens > 0 {
			returnProfiledStream(w, r, first, tail, profile, n)
			return
		}
		if !waitContext(r.Context(), first) {
			return
		}
		if !input.Stream {
			if !waitContext(r.Context(), tail) {
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"fixture","object":"chat.completion","model":"gpt-4.1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":32,"completion_tokens":2,"total_tokens":34}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4.1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\n")
		w.(http.Flusher).Flush()
		if !waitContext(r.Context(), tail) {
			return
		}
		_, _ = io.WriteString(w, "data: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4.1\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":32,\"completion_tokens\":2,\"total_tokens\":34}}\n\ndata: [DONE]\n\n")
	}))
}

func waitContext(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

type observation struct {
	Status              int
	Latency, First      float64
	OK                  bool
	Error               string
	Chunks, OutputBytes int
	MaxGap, Delivery    float64
	DeliveryMeasured    bool
}
type percentiles struct{ P50, P95, P99 float64 }
type loadResult struct {
	Profile                                                       profiledWorkload
	WireRequestBytes, VerifiedInputTokens, VerifiedOutputTokens   int
	ContentFrames, ContentBytes, DeliverySamples                  int
	MaxStreamGapMS, RelayDeliveryMS                               percentiles
	RampSeconds                                                   float64
	Concurrency, Users, PayloadBytes                              int
	Stream                                                        bool
	IssueSeconds, ElapsedSeconds                                  float64
	Requests, Successes                                           int
	Goodput, ErrorRate                                            float64
	Status                                                        map[int]int
	LatencyMS, FirstTextMS                                        percentiles
	GatewayCPUCoreAverage, GatewayCPUCoresPeak, GatewayRSSMiBPeak float64
	GeneratorCPUCoreAverage                                       float64
	SampleErrors                                                  []string
	UpstreamActive                                                percentiles
	UpstreamActivePeak                                            int
}

func summarize(values []float64) percentiles {
	if len(values) == 0 {
		return percentiles{}
	}
	sort.Float64s(values)
	at := func(q float64) float64 { return values[int(float64(len(values)-1)*q)] }
	return percentiles{at(.5), at(.95), at(.99)}
}

func runLoad(base string, clients, users int, duration, ramp time.Duration, stream bool, payload, pid int, output string, profile profiledWorkload) error {
	if err := loopbackURL(base); err != nil {
		return err
	}
	if clients < 1 || clients > 2048 || users < 1 || users > 128 || duration <= 0 || ramp < 0 || ramp >= duration || payload < 1 || payload > 1<<20 {
		return fmt.Errorf("invalid load dimensions")
	}
	if err := profile.validate(); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{"model": "gpt-4.1", "messages": []map[string]string{{"role": "user", "content": strings.Repeat("a ", (payload+1)/2)[:payload]}}, "stream": stream, "max_tokens": 32})
	path := "/v1/chat/completions"
	var verifiedInput, verifiedOutput int
	if profile.InputTokens > 0 {
		var err error
		body, path, verifiedInput, verifiedOutput, err = profile.requestBody()
		if err != nil {
			return err
		}
		stream = true
	}
	transport := &http.Transport{MaxIdleConns: clients * 2, MaxIdleConnsPerHost: clients, MaxConnsPerHost: clients, IdleConnTimeout: time.Minute}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: profile.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request := func(index int) observation {
		start := time.Now()
		r, err := http.NewRequest(http.MethodPost, strings.TrimRight(base, "/")+path, bytes.NewReader(body))
		if err != nil {
			return observation{Error: err.Error()}
		}
		r.Header.Set("Authorization", "Bearer sk-"+fixtureKey(index%users+1))
		r.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(r)
		if err != nil {
			return observation{Error: err.Error(), Latency: float64(time.Since(start).Microseconds()) / 1000}
		}
		defer resp.Body.Close()
		obs := observation{Status: resp.StatusCode}
		if resp.StatusCode != 200 {
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			obs.Error = string(raw)
		} else if profile.InputTokens > 0 {
			obs = readProfiledStream(resp.Body, start, profile)
			obs.Status = resp.StatusCode
		} else if stream {
			scanner := bufio.NewScanner(resp.Body)
			scanner.Buffer(make([]byte, 4096), 2<<20)
			var done bool
			for scanner.Scan() {
				line := scanner.Bytes()
				if !bytes.HasPrefix(line, []byte("data: ")) {
					continue
				}
				data := bytes.TrimSpace(line[6:])
				if bytes.Equal(data, []byte("[DONE]")) {
					done = true
					continue
				}
				var event struct {
					Choices []struct{ Delta struct{ Content string } }
					Error   json.RawMessage
				}
				if json.Unmarshal(data, &event) != nil {
					obs.Error = "invalid SSE JSON"
					continue
				}
				if len(event.Error) > 0 && string(event.Error) != "null" {
					obs.Error = string(event.Error)
				}
				for _, choice := range event.Choices {
					if choice.Delta.Content != "" && obs.First == 0 {
						obs.First = float64(time.Since(start).Microseconds()) / 1000
					}
				}
			}
			if err := scanner.Err(); err != nil {
				obs.Error = err.Error()
			}
			obs.OK = done && obs.First > 0 && obs.Error == ""
			if !obs.OK && obs.Error == "" {
				obs.Error = "incomplete SSE"
			}
		} else {
			var response struct {
				Choices []struct{ Message struct{ Content string } }
				Error   json.RawMessage
			}
			err := json.NewDecoder(resp.Body).Decode(&response)
			obs.OK = err == nil && len(response.Choices) > 0 && response.Choices[0].Message.Content != "" && len(response.Error) == 0
			if !obs.OK {
				obs.Error = fmt.Sprintf("invalid completion: %v %s", err, response.Error)
			}
			obs.First = float64(time.Since(start).Microseconds()) / 1000
		}
		obs.Latency = float64(time.Since(start).Microseconds()) / 1000
		return obs
	}
	// Verify a real billed completion before measuring. Failed paths are not throughput.
	if !profile.SkipWarmup {
		if obs := request(0); !obs.OK {
			return fmt.Errorf("warmup failed: status=%d %s", obs.Status, obs.Error)
		}
	}
	result := loadResult{Concurrency: clients, Users: users, PayloadBytes: payload, Stream: stream, Status: map[int]int{}, IssueSeconds: duration.Seconds()}
	result.Profile, result.WireRequestBytes = profile, len(body)
	result.VerifiedInputTokens, result.VerifiedOutputTokens = verifiedInput, verifiedOutput
	result.RampSeconds = ramp.Seconds()
	var gateway *process.Process
	if pid > 0 {
		var err error
		gateway, err = process.NewProcess(int32(pid))
		if err != nil {
			return err
		}
	}
	self, _ := process.NewProcess(int32(os.Getpid()))
	var gatewayStart, generatorStart float64
	if gateway != nil {
		if cpu, err := gateway.Times(); err == nil {
			gatewayStart = cpu.Total()
		}
	}
	if cpu, err := self.Times(); err == nil {
		generatorStart = cpu.Total()
	}
	sampleDone := make(chan struct{})
	var sampleWG sync.WaitGroup
	var activeSamples []float64
	statsClient := &http.Client{Timeout: time.Second}
	sampleWG.Add(1)
	go func() {
		defer sampleWG.Done()
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		last, lastTime := gatewayStart, time.Now()
		for {
			select {
			case <-sampleDone:
				return
			case now := <-ticker.C:
				if resp, err := statsClient.Get("http://127.0.0.1:18181/stats"); err == nil {
					var stats struct{ Active int }
					if err := json.NewDecoder(resp.Body).Decode(&stats); err == nil {
						activeSamples = append(activeSamples, float64(stats.Active))
						result.UpstreamActivePeak = max(result.UpstreamActivePeak, stats.Active)
					}
					_ = resp.Body.Close()
				}
				if gateway == nil {
					continue
				}
				if memory, err := gateway.MemoryInfo(); err == nil {
					result.GatewayRSSMiBPeak = max(result.GatewayRSSMiBPeak, float64(memory.RSS)/(1<<20))
				}
				if cpu, err := gateway.Times(); err == nil {
					result.GatewayCPUCoresPeak = max(result.GatewayCPUCoresPeak, (cpu.Total()-last)/now.Sub(lastTime).Seconds())
					last, lastTime = cpu.Total(), now
				}
			}
		}
	}()
	start := time.Now()
	deadline := start.Add(duration)
	observations := make(chan observation, clients*2)
	var wg sync.WaitGroup
	for worker := 0; worker < clients; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			if ramp > 0 {
				time.Sleep(time.Duration(int64(ramp) * int64(worker) / int64(clients)))
			}
			for index := worker; time.Now().Before(deadline); index += clients {
				obs := request(index)
				observations <- obs
				if !obs.OK {
					// Count the failure, then avoid a tight retry storm that changes
					// a concurrency test into an unbounded connection flood.
					time.Sleep(100 * time.Millisecond)
				}
			}
		}(worker)
	}
	go func() { wg.Wait(); close(observations) }()
	var latencies, first, gaps, delivery []float64
	for obs := range observations {
		result.Requests++
		result.Status[obs.Status]++
		result.ContentFrames += obs.Chunks
		result.ContentBytes += obs.OutputBytes
		if obs.DeliveryMeasured {
			delivery = append(delivery, obs.Delivery)
		}
		if obs.MaxGap > 0 {
			gaps = append(gaps, obs.MaxGap)
		}
		latencies = append(latencies, obs.Latency)
		if obs.OK {
			result.Successes++
			first = append(first, obs.First)
		} else if len(result.SampleErrors) < 3 {
			result.SampleErrors = append(result.SampleErrors, obs.Error)
		}
	}
	result.ElapsedSeconds = time.Since(start).Seconds()
	close(sampleDone)
	sampleWG.Wait()
	result.UpstreamActive = summarize(activeSamples)
	if gateway != nil {
		if cpu, err := gateway.Times(); err == nil {
			result.GatewayCPUCoreAverage = (cpu.Total() - gatewayStart) / result.ElapsedSeconds
		}
	}
	if cpu, err := self.Times(); err == nil {
		result.GeneratorCPUCoreAverage = (cpu.Total() - generatorStart) / result.ElapsedSeconds
	}
	result.Goodput = float64(result.Successes) / result.ElapsedSeconds
	result.ErrorRate = float64(result.Requests-result.Successes) / float64(max(1, result.Requests))
	result.LatencyMS = summarize(latencies)
	result.FirstTextMS = summarize(first)
	result.MaxStreamGapMS, result.RelayDeliveryMS = summarize(gaps), summarize(delivery)
	result.DeliverySamples = len(delivery)
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	if output != "" {
		return os.WriteFile(output, append(raw, '\n'), 0600)
	}
	return nil
}
