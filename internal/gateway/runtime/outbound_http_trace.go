package runtime

import (
	"crypto/tls"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"
)

type outboundHTTPTraceState struct {
	mu            sync.Mutex
	dnsStarts     []time.Time
	connectStarts map[string][]time.Time
	tlsStarts     []time.Time
}

// WithOutboundHTTPTrace attaches request-level connection and upload timing
// without recording the upstream address, request content, or credentials.
func WithOutboundHTTPTrace(req *http.Request, timing *FirstByteTrace, requestBodyBytes int64) *http.Request {
	if req == nil || timing == nil {
		return req
	}
	if !timing.beginOutboundTrace(requestBodyBytes) {
		return req
	}
	state := &outboundHTTPTraceState{connectStarts: make(map[string][]time.Time)}
	trace := &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) {
			state.pushDNS(time.Now())
		},
		DNSDone: func(httptrace.DNSDoneInfo) {
			if startedAt, ok := state.popDNS(); ok {
				timing.addOutboundDNSDuration(time.Since(startedAt))
			}
		},
		ConnectStart: func(network, addr string) {
			state.pushConnect(network, addr, time.Now())
		},
		ConnectDone: func(network, addr string, _ error) {
			if startedAt, ok := state.popConnect(network, addr); ok {
				timing.addOutboundConnectDuration(time.Since(startedAt))
			}
		},
		TLSHandshakeStart: func() {
			state.pushTLS(time.Now())
		},
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			if startedAt, ok := state.popTLS(); ok {
				timing.addOutboundTLSDuration(time.Since(startedAt))
			}
		},
		GotConn: func(info httptrace.GotConnInfo) {
			timing.markOutboundGotConn(time.Now(), info.Reused, info.WasIdle, info.IdleTime)
		},
		WroteRequest: func(httptrace.WroteRequestInfo) {
			timing.markOutboundWroteRequest(time.Now())
		},
		GotFirstResponseByte: func() {
			timing.markOutboundFirstResponseByte(time.Now())
		},
	}
	return req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
}

// MarkOutboundHTTPVersion records the protocol selected for the upstream hop.
func (t *FirstByteTrace) MarkOutboundHTTPVersion(major, minor int) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.outboundFirstByteAt.IsZero() || t.outboundHTTPProtoMajor > 0 {
		return
	}
	t.outboundHTTPProtoMajor = int64(major)
	t.outboundHTTPProtoMinor = int64(minor)
}

func (t *FirstByteTrace) beginOutboundTrace(size int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.outboundTraceMarked {
		return false
	}
	t.outboundTraceMarked = true
	if size > 0 {
		t.outboundRequestBodyBytes = size
	}
	return true
}

func (t *FirstByteTrace) addOutboundDNSDuration(duration time.Duration) {
	t.addOutboundDuration(&t.outboundDNSDuration, duration)
}

func (t *FirstByteTrace) addOutboundConnectDuration(duration time.Duration) {
	t.addOutboundDuration(&t.outboundConnectDuration, duration)
}

func (t *FirstByteTrace) addOutboundTLSDuration(duration time.Duration) {
	t.addOutboundDuration(&t.outboundTLSDuration, duration)
}

func (t *FirstByteTrace) addOutboundDuration(target *time.Duration, duration time.Duration) {
	if t == nil || duration < 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.outboundTraceMarked = true
	*target += duration
}

func (t *FirstByteTrace) markOutboundGotConn(at time.Time, reused, wasIdle bool, idleTime time.Duration) {
	if t == nil || at.IsZero() {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.outboundTraceMarked = true
	if t.outboundGotConnAt.IsZero() {
		t.outboundGotConnAt = at
		t.outboundConnReused = reused
		t.outboundConnWasIdle = wasIdle
		t.outboundConnIdleDuration = idleTime
		t.outboundConnMarked = true
	}
}

func (t *FirstByteTrace) markOutboundWroteRequest(at time.Time) {
	t.markOutboundTime(&t.outboundWroteRequestAt, at)
}

func (t *FirstByteTrace) markOutboundFirstResponseByte(at time.Time) {
	t.markOutboundTime(&t.outboundFirstByteAt, at)
}

func (t *FirstByteTrace) markOutboundTime(target *time.Time, at time.Time) {
	if t == nil || at.IsZero() {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.outboundTraceMarked = true
	if target.IsZero() {
		*target = at
	}
}

func (s *outboundHTTPTraceState) pushDNS(at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dnsStarts = append(s.dnsStarts, at)
}

func (s *outboundHTTPTraceState) popDNS() (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return popTraceStart(&s.dnsStarts)
}

func (s *outboundHTTPTraceState) pushConnect(network, addr string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := network + "\x00" + addr
	s.connectStarts[key] = append(s.connectStarts[key], at)
}

func (s *outboundHTTPTraceState) popConnect(network, addr string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := network + "\x00" + addr
	starts := s.connectStarts[key]
	startedAt, ok := popTraceStart(&starts)
	if len(starts) == 0 {
		delete(s.connectStarts, key)
	} else {
		s.connectStarts[key] = starts
	}
	return startedAt, ok
}

func (s *outboundHTTPTraceState) pushTLS(at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tlsStarts = append(s.tlsStarts, at)
}

func (s *outboundHTTPTraceState) popTLS() (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return popTraceStart(&s.tlsStarts)
}

func popTraceStart(starts *[]time.Time) (time.Time, bool) {
	if starts == nil || len(*starts) == 0 {
		return time.Time{}, false
	}
	index := len(*starts) - 1
	startedAt := (*starts)[index]
	*starts = (*starts)[:index]
	return startedAt, true
}
