package network

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const userAgent = "headview"

// AddDefaultProtocol adds https:// prefix if protocol is missing
func AddDefaultProtocol(s string) string {
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return "https://" + s
	}
	return s
}

// CreateHTTPClient creates an optimized HTTP client
func CreateHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
			MaxIdleConnsPerHost: 100,
			MaxConnsPerHost:     100,
			IdleConnTimeout:     30 * time.Second,
		},
		Timeout: 30 * time.Second,
	}
}

// PerformGetRequest performs HEAD requests, following redirects if needed
func PerformGetRequest(client *http.Client, urlArg string, headersArg bool) (*Timings, []ResponseInfo, error) {
	var timeStats Timings
	var responseInfos []ResponseInfo

	// Track visited URLs to detect redirect loops
	visited := make(map[string]bool)

	// Disable auto-redirect
	originalRedirectPolicy := client.CheckRedirect
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	// Restore original redirect policy when function exits
	defer func() {
		client.CheckRedirect = originalRedirectPolicy
	}()

	// Recursively follow redirects up to 10 times
	err := performGetRequestRecursive(client, urlArg, headersArg, &timeStats, &responseInfos, visited, 0, 10)

	return &timeStats, responseInfos, err
}

// performGetRequestRecursive is the recursive helper for PerformGetRequest
func performGetRequestRecursive(
	client *http.Client,
	urlArg string,
	headersArg bool,
	timeStats *Timings,
	responseInfos *[]ResponseInfo,
	visited map[string]bool,
	depth, maxDepth int,
) error {
	// Check for redirect loops
	if visited[urlArg] {
		return fmt.Errorf("redirect loop detected at URL: %s", urlArg)
	}

	// Check max redirect depth
	if depth > maxDepth {
		return fmt.Errorf("max redirect depth reached (%d)", maxDepth)
	}

	visited[urlArg] = true

	req, err := http.NewRequest("HEAD", urlArg, nil)
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("User-Agent", userAgent)

	// Create trace for timing information
	var times TimingsCommon
	trace := createHTTPTrace(&times)
	ctx, cancel := context.WithTimeout(httptrace.WithClientTrace(context.Background(), trace), 30*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	start := time.Now()
	requestSendingStart := time.Now()
	resp, err := client.Do(req)
	requestSendingTime := time.Since(requestSendingStart)

	if err != nil {
		return fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	// Add timing information
	timeStats.CommonTimings = append(timeStats.CommonTimings, times)

	// Calculate content size
	contentSize := int64(0)
	contentBuffer := new(bytes.Buffer)

	// Calculate content download time
	contentDownloadStart := time.Now()
	contentSize, err = io.Copy(contentBuffer, resp.Body)
	contentTransferTime := time.Since(contentDownloadStart)

	if err != nil {
		return fmt.Errorf("error reading response body: %w", err)
	}

	// Detect HTTP protocol and version
	proto := resp.Proto
	if proto != "" {
		times.HTTPVersion = proto
	}

	// Try to detect if HTTP/2 or HTTP/3
	if resp.ProtoMajor == 2 {
		times.Protocol = "HTTP/2"
	} else if resp.ProtoMajor == 3 {
		times.Protocol = "HTTP/3"
	} else if resp.TLS != nil {
		// HTTPS with HTTP/1.x
		times.Protocol = "HTTPS"
	} else {
		times.Protocol = "HTTP"
	}

	// Store response information
	*responseInfos = append(*responseInfos, ResponseInfo{
		Response:           resp,
		URL:                urlArg,
		ContentSize:        contentSize,
		RequestSendingTime: requestSendingTime,
		StartTime:          start,
	})

	// Set timing information for the last request
	if len(*responseInfos) == 1 {
		timeStats.RequestSendingTime = requestSendingTime
		ttfb := time.Since(start)
		timeStats.ServerProcessingTime = ttfb - requestSendingTime
		timeStats.TotalRequestTime = time.Since(start)
		timeStats.ContentTransferTime = contentTransferTime
	}

	// Check if a redirect response is received
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		location, err := resp.Location()
		if err != nil {
			return fmt.Errorf("error reading redirect location: %w", err)
		}

		// Follow the redirect
		return performGetRequestRecursive(client, location.String(), headersArg, timeStats, responseInfos, visited, depth+1, maxDepth)
	}

	return nil
}

// createHTTPTrace creates a trace to collect timing information
func createHTTPTrace(times *TimingsCommon) *httptrace.ClientTrace {
	var waitForServerStart time.Time
	var mu sync.Mutex

	return &httptrace.ClientTrace{
		// DNS events
		DNSStart: func(info httptrace.DNSStartInfo) {
			mu.Lock()
			times.DNSLookupStart = time.Now()
			mu.Unlock()
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			mu.Lock()
			if !times.DNSLookupStart.IsZero() {
				times.DNSLookupTime = time.Since(times.DNSLookupStart)
			}
			mu.Unlock()
		},

		// Connection events
		ConnectStart: func(network, addr string) {
			mu.Lock()
			times.TCPConnectStart = time.Now()
			mu.Unlock()
		},
		ConnectDone: func(network, addr string, err error) {
			mu.Lock()
			if err == nil && !times.TCPConnectStart.IsZero() {
				times.TCPConnTime = time.Since(times.TCPConnectStart)

				// Store connection address information if available
				if strings.Contains(addr, ":") {
					// Remove port number from address
					parts := strings.Split(addr, ":")
					if len(parts) > 0 {
						times.RemoteAddr = parts[0]
					}
				}
			}
			mu.Unlock()
		},

		// TLS events
		TLSHandshakeStart: func() {
			mu.Lock()
			times.TLSHandshakeStart = time.Now()
			mu.Unlock()
		},
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			mu.Lock()
			if !times.TLSHandshakeStart.IsZero() {
				times.TLSHandshakeTime = time.Since(times.TLSHandshakeStart)

				// Record TLS information
				times.TLSVersion = getTLSVersion(state.Version)
				times.TLSCipherSuite = getTLSCipherSuite(state.CipherSuite)
				times.TLSResumption = state.DidResume
			}
			mu.Unlock()

			// After TLS handshake, we're waiting for the server
			mu.Lock()
			waitForServerStart = time.Now()
			mu.Unlock()
		},

		// Connection reuse information
		GetConn: func(hostPort string) {
			// Connection tracking starts
		},
		GotConn: func(info httptrace.GotConnInfo) {
			mu.Lock()
			times.ConnectionReused = info.Reused
			if info.Conn != nil {
				times.LocalAddr = info.Conn.LocalAddr().String()
				times.RemoteAddr = info.Conn.RemoteAddr().String()
			}
			mu.Unlock()
		},

		// HTTP protocol information
		WroteHeaderField: func(key string, value []string) {
			if key == "User-Agent" {
				mu.Lock()
				// This is where we could record request header info if needed
				mu.Unlock()
			}
		},
		WroteHeaders: func() {
			// Headers have been written
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			// Request has been written
		},

		// Response events
		GotFirstResponseByte: func() {
			mu.Lock()
			times.FirstByteTime = time.Now()
			if !waitForServerStart.IsZero() {
				times.WaitingForServerTime = time.Since(waitForServerStart)
			}
			if !times.TCPConnectStart.IsZero() {
				// TTFB from initial connection start
				times.TTFB = time.Since(times.TCPConnectStart)
			}
			mu.Unlock()
		},
	}
}

// getTLSVersion converts TLS version code to string
func getTLSVersion(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("Unknown (0x%04x)", version)
	}
}

// getTLSCipherSuite converts cipher suite code to string
func getTLSCipherSuite(cipher uint16) string {
	// Map common cipher suites to their names
	cipherMap := map[uint16]string{
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256:   "ECDHE-RSA-AES128-GCM-SHA256",
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384:   "ECDHE-RSA-AES256-GCM-SHA384",
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256: "ECDHE-ECDSA-AES128-GCM-SHA256",
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384: "ECDHE-ECDSA-AES256-GCM-SHA384",
		tls.TLS_RSA_WITH_AES_128_GCM_SHA256:         "RSA-AES128-GCM-SHA256",
		tls.TLS_RSA_WITH_AES_256_GCM_SHA384:         "RSA-AES256-GCM-SHA384",
		tls.TLS_AES_128_GCM_SHA256:                  "TLS13-AES-128-GCM-SHA256",
		tls.TLS_AES_256_GCM_SHA384:                  "TLS13-AES-256-GCM-SHA384",
		tls.TLS_CHACHA20_POLY1305_SHA256:            "TLS13-CHACHA20-POLY1305-SHA256",
	}

	if name, ok := cipherMap[cipher]; ok {
		return name
	}
	return fmt.Sprintf("Unknown (0x%04x)", cipher)
}

// PerformGetSize performs the size calculation for a URL and its resources
func PerformGetSize(client *http.Client, urlArg string, concurrency int) (ResourceMap, error) {
	req, err := http.NewRequest("GET", urlArg, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request for size calculation: %w", err)
	}

	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request for size calculation: %w", err)
	}
	defer resp.Body.Close()

	return calculateSize(resp, client, concurrency)
}

// calculateSize calculates the size of all resources on the page
func calculateSize(resp *http.Response, client *http.Client, concurrency int) (ResourceMap, error) {
	resourceMap := make(ResourceMap)
	resourceLock := sync.Mutex{}

	baseURL, err := url.Parse(resp.Request.URL.String())
	if err != nil {
		return nil, fmt.Errorf("error parsing base URL: %w", err)
	}

	// Create a buffered reader to avoid loading the entire body into memory
	bodyBuffer := new(bytes.Buffer)
	size, err := io.Copy(bodyBuffer, resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}

	// Add the page itself as a resource
	pageResource := Resource{
		URL:  resp.Request.URL.String(),
		Size: size,
		Type: resp.Header.Get("Content-Type"),
	}

	resourceLock.Lock()
	resourceMap[pageResource.Type] = append(resourceMap[pageResource.Type], pageResource)
	resourceLock.Unlock()

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(bodyBuffer.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("error parsing HTML: %w", err)
	}

	// Find links to other resources
	var resourceURLs []string
	doc.Find("link[href], script[src], img[src]").Each(func(_ int, s *goquery.Selection) {
		link, exists := s.Attr("href")
		if !exists {
			link, exists = s.Attr("src")
		}

		if exists {
			resourceURLs = append(resourceURLs, link)
		}
	})

	// Use a semaphore to limit concurrent requests
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	errs := make(chan error, len(resourceURLs))

	for _, link := range resourceURLs {
		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore

		go func(link string) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			resource, err := fetchResource(link, baseURL, client)
			if err != nil {
				errs <- err
				return
			}

			if resource != nil {
				resourceLock.Lock()
				resourceMap[resource.Type] = append(resourceMap[resource.Type], *resource)
				resourceLock.Unlock()
			}
		}(link)
	}

	wg.Wait()
	close(errs)

	// Check if there were any errors
	for err := range errs {
		// Just log errors but don't fail the entire operation
		fmt.Printf("Warning: %v\n", err)
	}

	return resourceMap, nil
}

// fetchResource fetches a single resource and returns its size information
func fetchResource(link string, baseURL *url.URL, client *http.Client) (*Resource, error) {
	resourceURL, err := url.Parse(link)
	if err != nil {
		return nil, fmt.Errorf("error parsing resource URL %s: %w", link, err)
	}

	fullURL := baseURL.ResolveReference(resourceURL)
	req, err := http.NewRequest("GET", fullURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request for resource %s: %w", fullURL.String(), err)
	}

	req.Header.Set("User-Agent", userAgent)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error fetching resource %s: %w", fullURL.String(), err)
	}
	defer resp.Body.Close()

	// Use io.Copy to avoid loading the entire body into memory
	bodyBuffer := new(bytes.Buffer)
	size, err := io.Copy(bodyBuffer, resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading resource body %s: %w", fullURL.String(), err)
	}

	return &Resource{
		URL:  fullURL.String(),
		Size: size,
		Type: resp.Header.Get("Content-Type"),
	}, nil
}
