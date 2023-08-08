package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/guptarohit/asciigraph"
	"github.com/logrusorgru/aurora"
)

type timmings struct {
	CommonTimmings       []timmingsCommon
	RequestSendingTime   time.Duration
	ServerProcessingTime time.Duration
	TotalRequestTime     time.Duration
	ContentTransferTime  time.Duration
}

type timmingsCommon struct {
	DNSLookupTime    time.Duration
	TCPConnTime      time.Duration
	TLSHandshakeTime time.Duration
	TTFB             time.Duration
}

type resource struct {
	URL  string
	Size int64
	Type string
}

var appVersion = "0.1.15"
var timeStats timmings

func main() {
	// Check if URL is provided
	if len(os.Args) < 2 {
		fmt.Println("Please provide a URL as the first argument.")
		return
	}

	// Get URL from the first argument
	urlArg := addDefaultProtocol(os.Args[1])

	// Create a new flag set to parse the remaining arguments
	flags := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	// Define the rest of your flags
	headersArg := flags.Bool("headers", false, "Print headers")
	sizeArg := flags.Bool("size", false, "Calculate size of resources")
	verArg := flags.Bool("v", false, "Print version information")

	// Parse the remaining command line arguments
	flags.Parse(os.Args[2:])

	if *verArg {
		fmt.Printf(aurora.Sprintf(aurora.Green("headview v%s\n"), aurora.Yellow(appVersion)))
		return
	}

	client := createHTTPClient()

	if *sizeArg {
		performGetSize(client, urlArg)
	} else {
		performGetRequest(client, urlArg, *headersArg)

		fmt.Println("Connection")

		//print time stats

		//Connection Timmings
		if len(timeStats.CommonTimmings) > 1 {
			for _, t := range timeStats.CommonTimmings {
				fmt.Printf("%20s %-10s\n", aurora.Yellow("DNS lookup"), formatDuration(t.DNSLookupTime))
				fmt.Printf("%20s %-10s\n", aurora.Yellow("TCP connection"), formatDuration(t.TCPConnTime))
				fmt.Printf("%20s %-10s\n", aurora.Yellow("TLS handshake"), formatDuration(t.TLSHandshakeTime))
				fmt.Printf("%20s %-10s\n", aurora.Yellow("Time To First Byte"), formatDuration(t.TTFB))
				fmt.Println()
			}

		} else {
			reqgraph := asciigraph.Plot(timeStats.ExtractConnectionDurations())

			fmt.Printf("%20s %-10s\n", aurora.Yellow("DNS lookup"), formatDuration(timeStats.CommonTimmings[0].DNSLookupTime))
			fmt.Printf("%20s %-10s\n", aurora.Yellow("TCP connection"), formatDuration(timeStats.CommonTimmings[0].TCPConnTime))
			fmt.Printf("%20s %-10s\n", aurora.Yellow("TLS handshake"), formatDuration(timeStats.CommonTimmings[0].TLSHandshakeTime))
			fmt.Printf("%20s %-10s\n", aurora.Yellow("TTFB"), formatDuration(timeStats.CommonTimmings[0].TTFB))

			fmt.Println(reqgraph)
			fmt.Println()
		}

		//Request Timmings
		fmt.Println("Request")
		reqgraph := asciigraph.Plot(timeStats.ExtractDurations())

		fmt.Printf("%20s %-10s\n", aurora.Yellow("Request sending"), formatDuration(timeStats.RequestSendingTime))
		fmt.Printf("%20s %-10s\n", aurora.Yellow("Server processing"), formatDuration(timeStats.ServerProcessingTime))
		fmt.Printf("%20s %-10s\n", aurora.Yellow("Content transfer"), formatDuration(timeStats.ContentTransferTime))

		fmt.Println(reqgraph)

		fmt.Println()
		fmt.Printf("%20s %-10s\n", aurora.Yellow("Total request"), formatDuration(timeStats.TotalRequestTime))
	}
}

func (t *timmings) ExtractConnectionDurations() []float64 {
	var durations []float64
	for _, common := range t.CommonTimmings {
		durations = append(durations,
			common.DNSLookupTime.Seconds(),
			common.TCPConnTime.Seconds(),
			common.TLSHandshakeTime.Seconds(),
			common.TTFB.Seconds(),
		)
	}
	return durations
}

func (t *timmings) ExtractDurations() []float64 {
	var durations []float64
	durations = append(durations,
		t.RequestSendingTime.Seconds(),
		t.ServerProcessingTime.Seconds(),
		t.TotalRequestTime.Seconds(),
		t.ContentTransferTime.Seconds(),
	)
	return durations
}

// StringsToFloats converts a slice of strings to a slice of float64.
// For each string that can't be converted, it appends 0 to the float slice.
// Only the last error encountered is returned (if any).
func stringsToFloats(s []string) ([]float64, error) {
	var floats []float64
	var lastError error

	for _, str := range s {
		val, err := strconv.ParseFloat(str, 64)
		if err != nil {
			lastError = fmt.Errorf("failed to convert string %q to float: %v", str, err)
			val = 0.0
		}
		floats = append(floats, val)
	}

	return floats, lastError
}

func addDefaultProtocol(s string) string {
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return "https://" + s
	}
	return s
}

func createHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
}

func performGetRequest(client *http.Client, urlArg string, headersArg bool) {
	req, err := http.NewRequest("HEAD", urlArg, nil)
	if err != nil {
		fmt.Println(aurora.Green("Error creating request:"), aurora.Blue(err))
		return
	}

	fmt.Println(aurora.Magenta("Requesting URL:"), aurora.Cyan(urlArg))

	// Disable auto-redirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	start := time.Now()
	trace := createHTTPTrace()
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	requestSendingStart := time.Now()
	resp, err := client.Do(req)
	requestSendingTime := time.Since(requestSendingStart)

	if err != nil {
		fmt.Println(aurora.Red("Error sending request:"), aurora.Red(err))
		return
	}
	defer resp.Body.Close()

	// Check if a redirect response is received
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		location, err := resp.Location()
		if err != nil {
			fmt.Println(aurora.Red("Error reading redirect location:"), aurora.Red(err))
			return
		}
		fmt.Println(aurora.Magenta("Redirecting to:"), aurora.Cyan(location.String()))
		performGetRequest(client, location.String(), headersArg)
	} else {
		printResponse(start, resp, requestSendingTime, headersArg)
	}
}

func formatDuration(d time.Duration) string {
	durationStr := d.String()
	re := regexp.MustCompile(`([0-9\.]+)(\D+)`)
	matches := re.FindStringSubmatch(durationStr)

	if len(matches) < 3 {
		return durationStr
	}

	durationVal, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return durationStr
	}

	formattedDurationVal := fmt.Sprintf("%.2f", durationVal)
	return fmt.Sprintf("%s%s", formattedDurationVal, matches[2])
}

func createHTTPTrace() *httptrace.ClientTrace {
	var traceStart, connect, dns, tlsHandshake time.Time
	var times timmingsCommon

	return &httptrace.ClientTrace{
		DNSStart: func(_ httptrace.DNSStartInfo) {
			dns = time.Now()
			fmt.Println(aurora.Magenta("DNS lookup started."))
		},
		DNSDone: func(_ httptrace.DNSDoneInfo) {
			times.DNSLookupTime = time.Since(dns)
		},
		ConnectStart: func(_, _ string) {
			connect = time.Now()
			fmt.Println(aurora.Magenta("TCP connection started."))
		},
		ConnectDone: func(_, _ string, err error) {
			if err != nil {
				fmt.Printf("Error during connection: %v\n", err)
				return
			}
			times.TCPConnTime = time.Since(connect)
		},
		TLSHandshakeStart: func() {
			tlsHandshake = time.Now()
			fmt.Println(aurora.Magenta("TLS handshake started."))
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			times.TLSHandshakeTime = time.Since(tlsHandshake)
		},
		GotFirstResponseByte: func() {
			traceStart = time.Now()
			fmt.Println(aurora.Magenta("Received first response byte."))
			times.TTFB = time.Since(traceStart)

			//assuming last activity is reading the body so we append
			timeStats.CommonTimmings = append(timeStats.CommonTimmings, times)
		},
	}
}

func printResponse(start time.Time, resp *http.Response, requestSendingTime time.Duration, headersArg bool) {
	ttfb := time.Since(start)
	serverProcessingTime := ttfb - requestSendingTime

	timeStats.RequestSendingTime = requestSendingTime
	timeStats.ServerProcessingTime = serverProcessingTime
	timeStats.TotalRequestTime = time.Since(start)

	fmt.Println()
	fmt.Println(aurora.Green("Response status:"), aurora.Blue(resp.Status))
	if lastMod, ok := resp.Header["Last-Modified"]; ok {
		fmt.Println(aurora.Green("Last Modified:"), aurora.Blue(lastMod))
	} else {
		fmt.Println(aurora.Green("Last Modified header not present"))
	}
	fmt.Println()

	if headersArg {
		fmt.Println(aurora.Green("Response headers:"))
		for key, values := range resp.Header {
			for _, value := range values {
				fmt.Println(aurora.Green(key+": "), aurora.Blue(value))
			}
		}
	}

	// Calculate content download time
	contentDownloadStart := time.Now()
	_, err := io.ReadAll(resp.Body)
	contentTransferTime := time.Since(contentDownloadStart)
	if err != nil {
		fmt.Println(aurora.Red("Error reading response body:"), aurora.Red(err))
		return
	}

	timeStats.ContentTransferTime = contentTransferTime
}
