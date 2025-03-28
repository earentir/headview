package network

import (
	"net/http"
	"time"
)

// Timings holds all timing information for requests
type Timings struct {
	CommonTimings        []TimingsCommon
	RequestSendingTime   time.Duration
	ServerProcessingTime time.Duration
	TotalRequestTime     time.Duration
	ContentTransferTime  time.Duration
}

// TimingsCommon holds connection timing information
type TimingsCommon struct {
	// DNS phases
	DNSLookupStart time.Time
	DNSLookupTime  time.Duration

	// TCP connection phases
	TCPConnectStart time.Time
	TCPConnTime     time.Duration

	// TLS phases
	TLSHandshakeStart time.Time
	TLSHandshakeTime  time.Duration
	TLSVersion        string
	TLSCipherSuite    string
	TLSResumption     bool

	// Wait phases
	WaitingForServerTime time.Duration

	// First byte
	FirstByteTime time.Time
	TTFB          time.Duration

	// IP addresses
	LocalAddr  string
	RemoteAddr string

	// Connection reuse tracking
	ConnectionReused bool

	// Protocol information
	Protocol    string
	HTTPVersion string
}

// Resource represents a web resource with size information
type Resource struct {
	URL  string
	Size int64
	Type string
}

// ResourceMap is a map of content type to resources
type ResourceMap map[string][]Resource

// ResponseInfo holds information about an HTTP response
type ResponseInfo struct {
	Response           *http.Response
	URL                string
	ContentSize        int64
	RequestSendingTime time.Duration
	StartTime          time.Time
}

// ExtractConnectionDurations extracts connection durations for graphing
func (t *Timings) ExtractConnectionDurations() []float64 {
	durations := make([]float64, 0, len(t.CommonTimings)*4)
	for _, common := range t.CommonTimings {
		durations = append(durations,
			common.DNSLookupTime.Seconds(),
			common.TCPConnTime.Seconds(),
			common.TLSHandshakeTime.Seconds(),
			common.TTFB.Seconds(),
		)
	}
	return durations
}

// ExtractDurations extracts request durations for graphing
func (t *Timings) ExtractDurations() []float64 {
	return []float64{
		t.RequestSendingTime.Seconds(),
		t.ServerProcessingTime.Seconds(),
		t.ContentTransferTime.Seconds(),
	}
}
