package monitor

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"time"

	"domainradar/internal/domain"
)

// ProbeResult holds detailed timing info from a probe.
type ProbeResult struct {
	Success        bool
	ResponseTimeMs int64
	DNSMs          int64
	TCPMs          int64
	TLSMs          int64
	TTFBMs         int64
	DownloadMs     int64
	TotalMs        int64
	StatusCode     int
	ConnectedIP    string
	Error          string
}

// RunProbe executes the appropriate probe for a monitor and returns a ServiceCheck.
func RunProbe(monitor *domain.ServiceMonitor) domain.ServiceCheck {
	timeout := time.Duration(monitor.TimeoutSec) * time.Second
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	var result ProbeResult

	switch monitor.MonitorType {
	case "tcp":
		result = probeTCP(monitor.Target, timeout)
	case "udp":
		result = probeUDP(monitor.Target, timeout)
	case "http":
		result = probeHTTPDetailed(monitor.Target, timeout, monitor.ExpectedStatus, false)
	case "https":
		result = probeHTTPDetailed(monitor.Target, timeout, monitor.ExpectedStatus, true)
	default:
		result = ProbeResult{Error: "unsupported monitor type: " + monitor.MonitorType}
	}

	return domain.ServiceCheck{
		MonitorID:      monitor.ID,
		DomainID:       monitor.DomainID,
		Success:        result.Success,
		ResponseTimeMs: result.ResponseTimeMs,
		DNSMs:          result.DNSMs,
		TCPMs:          result.TCPMs,
		TLSMs:         result.TLSMs,
		TTFBMs:         result.TTFBMs,
		DownloadMs:     result.DownloadMs,
		TotalMs:        result.TotalMs,
		StatusCode:     result.StatusCode,
		ConnectedIP:    result.ConnectedIP,
		Error:          result.Error,
		CheckedAt:      time.Now(),
	}
}

// probeTCP performs a TCP connection test.
func probeTCP(target string, timeout time.Duration) ProbeResult {
	start := time.Now()

	// DNS resolution
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return ProbeResult{Error: fmt.Sprintf("invalid target: %v", err), TotalMs: time.Since(start).Milliseconds()}
	}

	dnsStart := time.Now()
	ips, err := net.LookupHost(host)
	dnsMs := time.Since(dnsStart).Milliseconds()
	if err != nil {
		return ProbeResult{Error: fmt.Sprintf("dns failed: %v", err), DNSMs: dnsMs, TotalMs: time.Since(start).Milliseconds()}
	}

	ip := ""
	if len(ips) > 0 {
		ip = ips[0]
	}

	// TCP connect
	tcpStart := time.Now()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, port), timeout)
	tcpMs := time.Since(tcpStart).Milliseconds()
	totalMs := time.Since(start).Milliseconds()

	if err != nil {
		return ProbeResult{Error: fmt.Sprintf("tcp connect failed: %v", err), DNSMs: dnsMs, TCPMs: tcpMs, TotalMs: totalMs, ConnectedIP: ip}
	}
	conn.Close()

	return ProbeResult{
		Success:        true,
		ResponseTimeMs: totalMs,
		DNSMs:          dnsMs,
		TCPMs:          tcpMs,
		TotalMs:        totalMs,
		ConnectedIP:    ip,
	}
}

// probeUDP performs a UDP connectivity test.
func probeUDP(target string, timeout time.Duration) ProbeResult {
	start := time.Now()
	conn, err := net.DialTimeout("udp", target, timeout)
	totalMs := time.Since(start).Milliseconds()
	if err != nil {
		return ProbeResult{Error: fmt.Sprintf("udp dial failed: %v", err), TotalMs: totalMs}
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(timeout))
	conn.Write([]byte{0x00})

	return ProbeResult{
		Success:        true,
		ResponseTimeMs: totalMs,
		TotalMs:        totalMs,
	}
}

// probeHTTPDetailed performs an HTTP/HTTPS request with detailed timing via httptrace.
func probeHTTPDetailed(url string, timeout time.Duration, expectedStatus int, isTLS bool) ProbeResult {
	var dnsStart, dnsEnd time.Time
	var tcpStart, tcpEnd time.Time
	var tlsStart, tlsEnd time.Time
	var ttfbTime time.Time
	var connectedIP string

	start := time.Now()

	trace := &httptrace.ClientTrace{
		DNSStart: func(_ httptrace.DNSStartInfo) {
			dnsStart = time.Now()
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			dnsEnd = time.Now()
			if len(info.Addrs) > 0 {
				connectedIP = info.Addrs[0].String()
			}
		},
		ConnectStart: func(_, _ string) {
			tcpStart = time.Now()
		},
		ConnectDone: func(_, _ string, err error) {
			tcpEnd = time.Now()
		},
		TLSHandshakeStart: func() {
			tlsStart = time.Now()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			tlsEnd = time.Now()
		},
		GotFirstResponseByte: func() {
			ttfbTime = time.Now()
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), "GET", url, nil)
	if err != nil {
		return ProbeResult{Error: fmt.Sprintf("request creation failed: %v", err), TotalMs: time.Since(start).Milliseconds()}
	}
	req.Header.Set("User-Agent", "DomainRadar-Monitor/1.0")

	// Use a transport that bypasses proxy for monitoring probes.
	// The container may have HTTP_PROXY set for other purposes,
	// but probes should always connect directly to the target.
	transport := &http.Transport{
		Proxy:                 nil, // no proxy for probes
		TLSClientConfig:      &tls.Config{InsecureSkipVerify: false},
		MaxIdleConns:         10,
		IdleConnTimeout:      30 * time.Second,
		DisableKeepAlives:    true,
		TLSHandshakeTimeout:  timeout,
		ResponseHeaderTimeout: timeout,
	}

	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		totalMs := time.Since(start).Milliseconds()
		return ProbeResult{
			Error:       fmt.Sprintf("request failed: %v", err),
			DNSMs:       safeDuration(dnsStart, dnsEnd),
			TCPMs:       safeDuration(tcpStart, tcpEnd),
			TLSMs:       safeDuration(tlsStart, tlsEnd),
			TotalMs:     totalMs,
			ConnectedIP: connectedIP,
		}
	}

	// Read body to measure download time
	downloadStart := time.Now()
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	downloadMs := time.Since(downloadStart).Milliseconds()

	totalMs := time.Since(start).Milliseconds()

	// Calculate timing
	dnsMs := safeDuration(dnsStart, dnsEnd)
	tcpMs := safeDuration(tcpStart, tcpEnd)
	tlsMs := safeDuration(tlsStart, tlsEnd)
	var ttfbMs int64
	if !ttfbTime.IsZero() {
		ttfbMs = ttfbTime.Sub(start).Milliseconds()
	}

	result := ProbeResult{
		Success:        true,
		ResponseTimeMs: totalMs,
		DNSMs:          dnsMs,
		TCPMs:          tcpMs,
		TLSMs:          tlsMs,
		TTFBMs:         ttfbMs,
		DownloadMs:     downloadMs,
		TotalMs:        totalMs,
		StatusCode:     resp.StatusCode,
		ConnectedIP:    connectedIP,
	}

	// Check expected status
	if expectedStatus > 0 && resp.StatusCode != expectedStatus {
		result.Success = false
		result.Error = fmt.Sprintf("expected status %d, got %d", expectedStatus, resp.StatusCode)
	}

	return result
}

func safeDuration(start, end time.Time) int64 {
	if start.IsZero() || end.IsZero() {
		return 0
	}
	return end.Sub(start).Milliseconds()
}

// Legacy probe functions for backward compatibility
func ProbeTCP(target string, timeout time.Duration) (int64, error) {
	r := probeTCP(target, timeout)
	if r.Error != "" {
		return r.TotalMs, fmt.Errorf("%s", r.Error)
	}
	return r.TotalMs, nil
}

func ProbeUDP(target string, timeout time.Duration) (int64, error) {
	r := probeUDP(target, timeout)
	if r.Error != "" {
		return r.TotalMs, fmt.Errorf("%s", r.Error)
	}
	return r.TotalMs, nil
}

func ProbeHTTP(url string, timeout time.Duration, expectedStatus int) (int64, int, error) {
	r := probeHTTPDetailed(url, timeout, expectedStatus, false)
	if r.Error != "" {
		return r.TotalMs, r.StatusCode, fmt.Errorf("%s", r.Error)
	}
	return r.TotalMs, r.StatusCode, nil
}

func ProbeHTTPS(url string, timeout time.Duration, expectedStatus int) (int64, int, error) {
	r := probeHTTPDetailed(url, timeout, expectedStatus, true)
	if r.Error != "" {
		return r.TotalMs, r.StatusCode, fmt.Errorf("%s", r.Error)
	}
	return r.TotalMs, r.StatusCode, nil
}
