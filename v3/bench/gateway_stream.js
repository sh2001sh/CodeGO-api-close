// k6 load test for acceptance criteria 2-3 (plan.md).
//
//   # 1. start the mock upstream
//   go run ./bench/mockupstream/cmd -addr 127.0.0.1:18080
//   # 2a. M0 baseline: hit the mock directly
//   k6 run -e TARGET=http://127.0.0.1:18080 bench/gateway_stream.js
//   # 2b. M1+: hit the v3 gateway, which routes to the mock
//   k6 run -e TARGET=http://127.0.0.1:3000 -e API_KEY=sk-... bench/gateway_stream.js
//
// Gateway overhead itself is read from codego_gateway_overhead_seconds on
// /metrics; k6 measures what clients see end to end.
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend, Rate } from 'k6/metrics';

const TARGET = __ENV.TARGET || 'http://127.0.0.1:18080';
// KEYS_FILE: JSON array of API keys from bench/seed; each VU uses one, so
// load spreads over many users and wallets like real traffic.
const KEYS = __ENV.KEYS_FILE ? JSON.parse(open(__ENV.KEYS_FILE)) : [__ENV.API_KEY || 'sk-bench'];
const STREAMS = Number(__ENV.STREAMS || 5000);
const RPS = Number(__ENV.RPS || 2000);

const ttfb = new Trend('stream_ttfb', true);
const complete = new Rate('stream_complete');

export const options = {
  scenarios: {
    streams: {
      executor: 'constant-vus',
      vus: STREAMS,
      duration: __ENV.DURATION || '5m',
      exec: 'streaming',
    },
    non_streaming: {
      executor: 'constant-arrival-rate',
      rate: RPS,
      timeUnit: '1s',
      duration: __ENV.DURATION || '5m',
      preAllocatedVUs: 500,
      maxVUs: 4000,
      exec: 'nonStreaming',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.001'], // acceptance #3: error rate < 0.1%
    stream_complete: ['rate>0.999'],
    // Client-observed TTFB includes load-generator jitter: hitting the mock
    // directly (no gateway) measured p99 59-81 ms against a 50 ms mock TTFB
    // on one Docker VM. It is a regression guard only. Acceptance #2 (gateway
    // overhead p99 <= 20 ms) is read from codego_gateway_overhead_seconds.
    stream_ttfb: [`p(50)<${Number(__ENV.TTFB_P50_MS || 60)}`, `p(99)<${Number(__ENV.TTFB_P99_MS || 120)}`],
  },
};

const body = JSON.stringify({
  model: 'mock',
  stream: true,
  max_tokens: 64,
  messages: [{ role: 'user', content: 'hello' }],
});
const jsonBody = JSON.stringify({ model: 'mock', max_tokens: 64, messages: [{ role: 'user', content: 'hello' }] });

function params() {
  return {
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${KEYS[__VU % KEYS.length]}` },
    timeout: '120s',
  };
}

// The query string only matters when TARGET is the mock itself; through the
// gateway the scenario is pinned by the channel's base URL.
export function streaming() {
  // Every stream has the same length, so without jitter all VUs stay in
  // phase and arrive as one burst per stream length. Real traffic does not.
  sleep(Math.random() * 0.5);
  const res = http.post(`${TARGET}/v1/chat/completions?scenario=complete&chunks=20&interval_ms=50`, body, params());
  ttfb.add(res.timings.waiting);
  const ok = check(res, {
    'status 200': (r) => r.status === 200,
    'ends with [DONE]': (r) => typeof r.body === 'string' && r.body.trimEnd().endsWith('data: [DONE]'),
    'has usage': (r) => typeof r.body === 'string' && r.body.includes('"usage"'),
  });
  complete.add(ok);
}

export function nonStreaming() {
  const res = http.post(`${TARGET}/v1/chat/completions?scenario=complete&chunks=1&interval_ms=0&ttfb_ms=5`, jsonBody, params());
  check(res, { 'status 200': (r) => r.status === 200 });
}
