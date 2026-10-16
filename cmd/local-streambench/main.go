// local-streambench measures synthetic long streams through disposable local gateways.
package main

import (
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

type benchConfig struct {
	URL, Protocol, Model, Route, Scenario, Out string
	Concurrency, Requests, Users, InputTokens  int
	TTFT, Tail, Interval, Ramp, Timeout        time.Duration
	TruncateEvery, ErrorEvery                  int
	PID                                        int
	ChunkBytes                                 int
}

func main() {
	c := benchConfig{}
	mode := flag.String("mode", "run", "seed, check, upstream, or run")
	flag.StringVar(&c.URL, "url", "http://127.0.0.1:18180", "explicit loopback HTTP target")
	flag.StringVar(&c.Protocol, "protocol", "chat", "chat, responses, claude, gemini, or ws")
	flag.StringVar(&c.Model, "model", "gpt-4.1", "fixture model or bridge alias")
	flag.StringVar(&c.Route, "route", "sb-openai-direct", "fixture token group; routing comes from its stored token")
	flag.StringVar(&c.Scenario, "scenario", "text", "text, thinking, or tool")
	flag.StringVar(&c.Out, "out", "", "result JSON path")
	flag.IntVar(&c.Concurrency, "c", 1, "concurrent workers")
	flag.IntVar(&c.Requests, "n", 1, "fixed total requests; no implicit warmup")
	flag.IntVar(&c.Users, "users", 128, "fixture users, rotated per request")
	flag.IntVar(&c.InputTokens, "input-tokens", 100000, "verified cl100k prompt-content tokens")
	flag.DurationVar(&c.TTFT, "ttft", 500*time.Millisecond, "mock delay before first semantic text")
	flag.DurationVar(&c.Tail, "tail", 6*time.Second, "mock duration after first semantic text")
	flag.DurationVar(&c.Interval, "interval", 100*time.Millisecond, "mock text frame interval")
	flag.DurationVar(&c.Ramp, "ramp", 0, "spread worker starts over this period")
	flag.DurationVar(&c.Timeout, "timeout", 3*time.Minute, "per-request deadline including long stream")
	flag.IntVar(&c.TruncateEvery, "truncate-every", 0, "mock truncates every Nth stream; 0 disables")
	flag.IntVar(&c.ErrorEvery, "error-every", 0, "mock emits failed terminal on every Nth stream")
	flag.IntVar(&c.PID, "pid", 0, "optional gateway PID for 250ms CPU/RSS sampling")
	flag.IntVar(&c.ChunkBytes, "chunk-bytes", 0, "extra ASCII bytes per text frame for long-output cases; 0 normal")
	flag.Parse()
	var err error
	switch *mode {
	case "seed":
		err = seedFixtures()
	case "check":
		err = checkFixtures()
	case "upstream":
		err = serveMock(c)
	case "run":
		err = runLoad(c)
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
	if u.Scheme != "http" || ip == nil || !ip.IsLoopback() || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("only explicit loopback HTTP origins without credentials, path, query or fragment are allowed")
	}
	return nil
}

func (c benchConfig) validate() error {
	if err := loopbackURL(c.URL); err != nil {
		return err
	}
	if !strings.Contains("|chat|responses|claude|gemini|ws|", "|"+c.Protocol+"|") || c.Model == "" || c.Route == "" ||
		(c.Scenario != "text" && c.Scenario != "thinking" && c.Scenario != "tool") || c.InputTokens < 16 || c.InputTokens > 200000 ||
		c.Concurrency < 1 || c.Concurrency > 2000 || c.Requests < 1 || c.Requests > 10000 || c.Users < 1 || c.Users > 128 ||
		c.TTFT < 0 || c.Tail < 0 || c.Tail > time.Minute || c.Interval <= 0 || c.Interval > time.Second || c.Ramp < 0 || c.Timeout <= 0 || c.TruncateEvery < 0 || c.ErrorEvery < 0 || c.ChunkBytes < 0 || c.ChunkBytes > 65536 {
		return fmt.Errorf("invalid benchmark configuration")
	}
	return nil
}
