package stream

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	platformgeneral "github.com/sh2001sh/new-api/internal/platform/general"
	"github.com/sh2001sh/new-api/internal/platform/logger"
)

const (
	InitialScannerBufferSize    = 64 << 10
	DefaultMaxScannerBufferSize = 64 << 20
	DefaultPingInterval         = 10 * time.Second
)

type scannedEvent struct {
	data       string
	receivedAt time.Time
}

func getScannerBufferSize() int {
	if constant.StreamScannerMaxBufferMB > 0 {
		return constant.StreamScannerMaxBufferMB << 20
	}
	return DefaultMaxScannerBufferSize
}

func NewStreamScanner(reader io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, InitialScannerBufferSize), getScannerBufferSize())
	return scanner
}

// ScanResponse coordinates three workers: upstream scanning, downstream data
// delivery, and optional pinging. A closed stop channel broadcasts abnormal
// cancellation; normal scanner completion closes dataChan and lets the data
// worker drain all already-read frames before returning.
func ScanResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, dataHandler func(data string, sr *Result)) {
	if resp == nil || dataHandler == nil || info == nil {
		return
	}

	info.StreamStatus = gatewaycontract.NewStreamStatus()
	defer func() {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()

	streamingTimeout := time.Duration(constant.StreamingTimeout) * time.Second
	firstByteTimeout := relaycommon.StreamFirstOutputTimeoutForRequest(c, info.OriginModelName, info.GetEstimatePromptTokens())
	streamingMaxDuration := relaycommon.StreamMaxDurationForRequest(c, info.OriginModelName, info.GetEstimatePromptTokens())
	streamCtx, cancelStream := context.WithCancel(c.Request.Context())
	dataCtx, cancelData := context.WithCancel(c.Request.Context())
	defer cancelStream()
	defer cancelData()

	stopChan := make(chan struct{})
	var stopOnce sync.Once
	stop := func(cancelDataWorker bool) {
		stopOnce.Do(func() {
			close(stopChan)
			cancelStream()
			if cancelDataWorker {
				cancelData()
			}
		})
	}
	SetStreamWorkerContext(c, dataCtx)

	scanner := NewStreamScanner(resp.Body)
	ticker := time.NewTicker(streamingTimeout)
	defer ticker.Stop()
	var firstByteTimer *time.Timer
	var maxTimer *time.Timer
	if firstByteTimeout > 0 {
		firstByteTimer = time.NewTimer(firstByteTimeout)
		defer firstByteTimer.Stop()
	}
	if streamingMaxDuration > 0 {
		maxTimer = time.NewTimer(streamingMaxDuration)
		defer maxTimer.Stop()
	}

	generalSettings := platformgeneral.GetSetting()
	pingEnabled := generalSettings.PingIntervalEnabled && !info.DisablePing
	pingInterval := time.Duration(generalSettings.PingIntervalSeconds) * time.Second
	if pingInterval <= 0 {
		pingInterval = DefaultPingInterval
	}
	var pingTicker *time.Ticker
	if pingEnabled {
		pingTicker = time.NewTicker(pingInterval)
		defer pingTicker.Stop()
	}

	scannerDone := make(chan struct{})
	dataDone := make(chan struct{})
	pingDone := make(chan struct{})
	dataChan := make(chan scannedEvent, 10)
	pingRequests := make(chan struct{}, 1)

	if pingEnabled && pingTicker != nil {
		gopool.Go(func() {
			defer close(pingDone)
			defer func() {
				if r := recover(); r != nil {
					logger.LogError(c, fmt.Sprintf("ping goroutine panic: %v", r))
					info.StreamStatus.SetEndReason(gatewaycontract.StreamEndReasonPanic, fmt.Errorf("ping panic: %v", r))
					stop(true)
				}
			}()

			for {
				select {
				case <-pingTicker.C:
					select {
					case pingRequests <- struct{}{}:
					default:
						// A pending ping is enough to keep an idle stream alive.
					case <-streamCtx.Done():
						return
					}
				case <-streamCtx.Done():
					return
				case <-c.Request.Context().Done():
					return
				}
			}
		})
	} else {
		close(pingDone)
	}

	gopool.Go(func() {
		defer close(dataDone)
		defer func() {
			if r := recover(); r != nil {
				logger.LogError(c, fmt.Sprintf("data handler goroutine panic: %v", r))
				info.StreamStatus.SetEndReason(gatewaycontract.StreamEndReasonPanic, fmt.Errorf("handler panic: %v", r))
				stop(true)
			}
		}()
		sr := newResult(info.StreamStatus)
		streamDone := streamCtx.Done()
		for {
			select {
			case <-dataCtx.Done():
				return
			case <-streamDone:
				// Normal scanner completion stops new pings, but queued upstream
				// frames must still be delivered before this worker returns.
				streamDone = nil
				pingRequests = nil
			case event, ok := <-dataChan:
				if !ok {
					return
				}
				sr.reset()
				sr.setReceivedAt(event.receivedAt)
				dataHandler(event.data, sr)
				if sr.IsStopped() {
					stop(true)
					return
				}
			case <-pingRequests:
				if err := PingData(c); err != nil {
					logger.LogError(c, "ping data error: "+err.Error())
					info.StreamStatus.SetEndReason(gatewaycontract.StreamEndReasonPingFail, err)
					stop(true)
					return
				}
			}
		}
	})

	scanner.Split(bufio.ScanLines)
	SetEventStreamHeaders(c)
	gopool.Go(func() {
		defer close(scannerDone)
		defer close(dataChan)
		defer func() {
			if r := recover(); r != nil {
				logger.LogError(c, fmt.Sprintf("scanner goroutine panic: %v", r))
				info.StreamStatus.SetEndReason(gatewaycontract.StreamEndReasonPanic, fmt.Errorf("scanner panic: %v", r))
				stop(true)
			}
		}()

		for scanner.Scan() {
			select {
			case <-stopChan:
				return
			case <-streamCtx.Done():
				return
			case <-c.Request.Context().Done():
				MarkClientGone(c)
				info.StreamStatus.SetEndReason(gatewaycontract.StreamEndReasonClientGone, c.Request.Context().Err())
				stop(true)
				return
			default:
			}

			ticker.Reset(streamingTimeout)
			data := scanner.Text()
			if len(data) < 6 {
				continue
			}
			if data[:5] != "data:" && data[:6] != "[DONE]" {
				continue
			}
			data = strings.TrimSpace(data[5:])
			if data == "" {
				continue
			}
			if strings.HasPrefix(data, "[DONE]") {
				info.StreamStatus.SetEndReason(gatewaycontract.StreamEndReasonDone, nil)
				return
			}
			if firstByteTimer != nil {
				if !firstByteTimer.Stop() {
					select {
					case <-firstByteTimer.C:
					default:
					}
				}
			}
			info.SetFirstResponseTime()
			info.ReceivedResponseCount++
			select {
			case dataChan <- scannedEvent{data: data, receivedAt: time.Now()}:
			case <-streamCtx.Done():
				return
			case <-c.Request.Context().Done():
				MarkClientGone(c)
				stop(true)
				return
			}
		}

		if err := scanner.Err(); err != nil {
			if err != io.EOF {
				logger.LogError(c, "scanner error: "+err.Error())
				info.StreamStatus.SetEndReason(gatewaycontract.StreamEndReasonScannerErr, err)
				stop(true)
				return
			}
		}
		info.StreamStatus.SetEndReason(gatewaycontract.StreamEndReasonEOF, nil)
	})

	normalScannerEnd := false
	select {
	case <-scannerDone:
		normalScannerEnd = info.StreamStatus.IsNormalEnd() && !info.StreamStatus.HasErrors()
		stop(false)
	case <-streamingFirstByteTimer(firstByteTimer):
		info.StreamStatus.SetEndReason(gatewaycontract.StreamEndReasonTimeout, fmt.Errorf("first byte timeout after %s", firstByteTimeout))
		stop(true)
		closeTimedOutStream(resp)
	case <-ticker.C:
		info.StreamStatus.SetEndReason(gatewaycontract.StreamEndReasonTimeout, nil)
		stop(true)
		closeTimedOutStream(resp)
	case <-streamingMaxTimer(maxTimer):
		info.StreamStatus.SetEndReason(gatewaycontract.StreamEndReasonMaxDuration, nil)
		relaycommon.MarkLocalStreamMaxDurationExceeded(c)
		stop(true)
		closeTimedOutStream(resp)
	case <-stopChan:
		// A handler, ping, or worker failure must interrupt a Scanner blocked in
		// an upstream Read. Otherwise it occupies a routing candidate until idle
		// timeout after the downstream attempt has already been abandoned.
		closeTimedOutStream(resp)
	case <-c.Request.Context().Done():
		MarkClientGone(c)
		info.StreamStatus.SetEndReason(gatewaycontract.StreamEndReasonClientGone, c.Request.Context().Err())
		stop(true)
	}

	if !normalScannerEnd {
		<-scannerDone
	}
	<-pingDone
	if normalScannerEnd {
		// The scanner has closed dataChan. Keep dataCtx alive while all already
		// read frames are delivered, then release the pacing context.
		<-dataDone
		cancelData()
	} else {
		cancelData()
		<-dataDone
	}

	if info.StreamStatus.IsNormalEnd() && !info.StreamStatus.HasErrors() {
		logger.LogInfo(c, fmt.Sprintf("stream ended: %s", info.StreamStatus.Summary()))
	} else {
		logger.LogError(c, fmt.Sprintf("stream ended: %s, received=%d", info.StreamStatus.Summary(), info.ReceivedResponseCount))
	}
}
