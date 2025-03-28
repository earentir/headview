package output

import (
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/guptarohit/asciigraph"
	"github.com/logrusorgru/aurora"

	"headview/network"
)

// PrintVersion prints the version information
func PrintVersion(version string) {
	fmt.Println(aurora.Sprintf(aurora.Green("headview v%s"), aurora.Yellow(version)))
}

// PrintResponseInfo prints information about an HTTP response
func PrintResponseInfo(respInfo network.ResponseInfo, isNewConnection bool, showHeaders bool) {
	resp := respInfo.Response

	// Print URL and status
	fmt.Println()
	fmt.Println(aurora.Magenta("URL:"), aurora.Cyan(respInfo.URL))
	fmt.Println(aurora.Green("Response status:"), aurora.Blue(resp.Status))

	// Print Last-Modified header if present
	if lastMod, ok := resp.Header["Last-Modified"]; ok {
		fmt.Println(aurora.Green("Last Modified:"), aurora.Blue(lastMod))
	} else {
		fmt.Println(aurora.Yellow("Last Modified header not present"))
	}

	// Print content size
	fmt.Printf("%20s %s\n", aurora.BrightGreen("Content size:"), aurora.Blue(FormatSize(respInfo.ContentSize)))

	// Print connection information
	if isNewConnection {
		fmt.Println(aurora.Green("New connection established for this request"))
	} else {
		fmt.Println(aurora.Green("Reused existing connection for this request"))
	}

	// Print headers if requested
	if showHeaders {
		fmt.Println()
		fmt.Println(aurora.Green("Response headers:"))
		for key, values := range resp.Header {
			for _, value := range values {
				fmt.Println(aurora.Green(key+": "), aurora.Blue(value))
			}
		}
	}
}

// PrintConnectionTiming prints timing information for a single connection
func PrintConnectionTiming(connectionNum int, timing network.TimingsCommon) {
	fmt.Println(aurora.Green(fmt.Sprintf("Connection #%d", connectionNum)))

	// Connection info
	if timing.ConnectionReused {
		fmt.Printf("%20s %s\n", aurora.BrightGreen("Connection"), aurora.Blue("Reused"))
	} else {
		fmt.Printf("%20s %s\n", aurora.BrightGreen("Connection"), aurora.Blue("New"))
	}

	// IP address info if available
	if timing.LocalAddr != "" || timing.RemoteAddr != "" {
		fmt.Printf("%20s %s\n", aurora.BrightGreen("Local address"), aurora.Blue(timing.LocalAddr))
		fmt.Printf("%20s %s\n", aurora.BrightGreen("Remote address"), aurora.Blue(timing.RemoteAddr))
	}

	// DNS timing
	fmt.Printf("%20s %s\n", aurora.BrightGreen("DNS lookup"), FormatDuration(timing.DNSLookupTime))

	// TCP timing
	fmt.Printf("%20s %s\n", aurora.BrightGreen("TCP connection"), FormatDuration(timing.TCPConnTime))

	// TLS info and timing
	if timing.TLSHandshakeTime > 0 {
		fmt.Printf("%20s %s\n", aurora.BrightGreen("TLS handshake"), FormatDuration(timing.TLSHandshakeTime))

		if timing.TLSVersion != "" {
			fmt.Printf("%20s %s\n", aurora.BrightGreen("TLS version"), aurora.Blue(timing.TLSVersion))
		}

		if timing.TLSCipherSuite != "" {
			fmt.Printf("%20s %s\n", aurora.BrightGreen("TLS cipher"), aurora.Blue(timing.TLSCipherSuite))
		}

		fmt.Printf("%20s %s\n", aurora.BrightGreen("TLS resumption"), aurora.Blue(fmt.Sprintf("%t", timing.TLSResumption)))
	}

	// Server wait time
	if timing.WaitingForServerTime > 0 {
		fmt.Printf("%20s %s\n", aurora.BrightGreen("Waiting for server"), FormatDuration(timing.WaitingForServerTime))
	}

	// TTFB
	fmt.Printf("%20s %s\n", aurora.BrightGreen("Time To First Byte"), FormatDuration(timing.TTFB))

	// Protocol info if available
	if timing.Protocol != "" {
		fmt.Printf("%20s %s\n", aurora.Yellow("Protocol"), aurora.Blue(timing.Protocol))
	}

	if timing.HTTPVersion != "" {
		fmt.Printf("%20s %s\n", aurora.Yellow("HTTP version"), aurora.Blue(timing.HTTPVersion))
	}

	fmt.Println()
}

// PrintCombinedTimingStats prints combined statistics about request timing
func PrintCombinedTimingStats(timeStats *network.Timings) {
	// Print visual graph of connection timings if there were multiple connections
	if len(timeStats.CommonTimings) > 1 {
		fmt.Println(aurora.Green("Connection Timings Comparison"))
		printConnectionTimingsGraph(timeStats)
	}

	// Request Timings
	fmt.Println(aurora.Green("Request"))
	reqgraph := asciigraph.Plot(timeStats.ExtractDurations())

	fmt.Printf("%20s %s\n", aurora.BrightGreen("Request sending"), FormatDuration(timeStats.RequestSendingTime))
	fmt.Printf("%20s %s\n", aurora.BrightGreen("Server processing"), FormatDuration(timeStats.ServerProcessingTime))
	fmt.Printf("%20s %s\n", aurora.BrightGreen("Content transfer"), FormatDuration(timeStats.ContentTransferTime))

	fmt.Println(reqgraph)

	fmt.Println()
	fmt.Printf("%20s %s\n", aurora.BrightGreen("Total request"), FormatDuration(timeStats.TotalRequestTime))
}

// printConnectionTimingsGraph prints a graph comparing multiple connection timings
func printConnectionTimingsGraph(timeStats *network.Timings) {
	var multireqgraph [][]float64

	for _, t := range timeStats.CommonTimings {
		multireqgraph = append(multireqgraph, []float64{
			t.DNSLookupTime.Seconds(),
			t.TCPConnTime.Seconds(),
			t.TLSHandshakeTime.Seconds(),
			t.TTFB.Seconds(),
		})
	}

	graph := asciigraph.PlotMany(multireqgraph, asciigraph.Height(10), asciigraph.SeriesColors(asciigraph.White, asciigraph.Blue))
	fmt.Println(graph)
	fmt.Println()
}

// PrintResourceSizes prints information about resource sizes
func PrintResourceSizes(resources network.ResourceMap) {
	var totalSize int64

	// Print each resource type
	for resType, resItems := range resources {
		fmt.Println(aurora.Green("Type:"), aurora.Blue(resType))
		var typeTotalSize int64

		// Print each resource
		for _, res := range resItems {
			fmt.Printf("%s %s\n", aurora.Green(res.URL), aurora.Blue(FormatSize(res.Size)))
			typeTotalSize += res.Size
			totalSize += res.Size
		}

		// Print subtotal for this type
		fmt.Printf("%s %s\n", aurora.Green("Total size for this type:"), aurora.Blue(FormatSize(typeTotalSize)))
		fmt.Println()
	}

	// Print overall total
	fmt.Printf("%s %s\n", aurora.Green("Total size for all resources:"), aurora.Blue(FormatSize(totalSize)))
}

// FormatDuration formats a duration in a more readable way
func FormatDuration(d time.Duration) string {
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

// FormatSize formats a byte size in a human-readable way
func FormatSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	} else if size < 1024*1024 {
		return fmt.Sprintf("%.2f KB", float64(size)/1024)
	} else if size < 1024*1024*1024 {
		return fmt.Sprintf("%.2f MB", float64(size)/(1024*1024))
	}
	return fmt.Sprintf("%.2f GB", float64(size)/(1024*1024*1024))
}

// PrintConnectionActivity prints connection activity messages
func PrintConnectionActivity(activity string) {
	fmt.Println(aurora.Magenta(activity))
}
