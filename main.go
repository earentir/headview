package main

import (
	"bytes"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/logrusorgru/aurora"
)

type resource struct {
	URL  string
	Size int64
	Type string
}

var appVersion = "0.1.9"

func main() {
	// Check if URL is provided
	if len(os.Args) < 2 {
		fmt.Println("Please provide a URL as the first argument.")
		return
	}

	// Get URL from the first argument
	urlArg := os.Args[1]

	// Create a new flag set to parse the remaining arguments
	flags := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	// Define the rest of your flags
	statisticsArg := flags.Bool("stats", false, "Print statistics Only")
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
		performGetRequest(client, urlArg, *statisticsArg)
	}
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

func performGetSize(client *http.Client, urlArg string) {
	req, err := http.NewRequest("GET", urlArg, nil)
	if err != nil {
		fmt.Println(aurora.Green("Error creating request for size calculation:"), aurora.Blue(err))
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(aurora.Red("Error sending request for size calculation:"), aurora.Red(err))
		return
	}
	defer resp.Body.Close()

	calculateSize(resp, client)
}

func performGetRequest(client *http.Client, urlArg string, statisticsArg bool) {
	req, err := http.NewRequest("HEAD", urlArg, nil)
	if err != nil {
		fmt.Println(aurora.Green("Error creating request:"), aurora.Blue(err))
		return
	}

	start := time.Now()
	trace := createHTTPTrace()
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	requestSendingStart := time.Now()
	resp, err := client.Do(req)
	requestSendingTime := time.Since(requestSendingStart)

	if err != nil {
		fmt.Println(aurora.Red("Error sending request:"), aurora.Red(err))
	} else {
		defer resp.Body.Close()
		printResponse(start, resp, requestSendingTime, statisticsArg)
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
	return &httptrace.ClientTrace{
		DNSStart: func(_ httptrace.DNSStartInfo) { dns = time.Now() },
		DNSDone: func(_ httptrace.DNSDoneInfo) {
			fmt.Printf("%25s %-10s\n", aurora.Yellow("DNS lookup duration"), formatDuration(time.Since(dns)))

		},
		ConnectStart: func(_, _ string) { connect = time.Now() },
		ConnectDone: func(_, _ string, err error) {
			if err != nil {
				fmt.Printf("Error during connection: %v\n", err)
				return
			}
			fmt.Printf("%25s %-10s\n", aurora.Yellow("TCP connection duration"), formatDuration(time.Since(connect)))
		},
		TLSHandshakeStart: func() { tlsHandshake = time.Now() },
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			fmt.Printf("%25s %-10s\n", aurora.Yellow("TLS handshake duration"), formatDuration(time.Since(tlsHandshake)))
		},
		GotFirstResponseByte: func() {
			traceStart = time.Now()
			fmt.Printf("%25s %-10s\n", aurora.Yellow("Time to first byte"), formatDuration(time.Since(traceStart)))
		},
	}
}

func printResponse(start time.Time, resp *http.Response, requestSendingTime time.Duration, statisticsArg bool) {
	ttfb := time.Since(start)
	serverProcessingTime := ttfb - requestSendingTime

	fmt.Printf("%25s %-10s\n", aurora.Yellow("Request sending time"), formatDuration(requestSendingTime))
	fmt.Printf("%25s %-10s\n", aurora.Yellow("Server processing time"), formatDuration(serverProcessingTime))
	fmt.Printf("%25s %-10s\n", aurora.Yellow("Total request duration"), formatDuration(time.Since(start)))

	fmt.Println()
	fmt.Println(aurora.Green("Response status:"), aurora.Blue(resp.Status))
	if lastMod, ok := resp.Header["Last-Modified"]; ok {
		fmt.Println(aurora.Green("Last Modified:"), aurora.Blue(lastMod))
	} else {
		fmt.Println(aurora.Green("Last Modified header not present"))
	}
	fmt.Println()

	if !statisticsArg {
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
	contentDownloadTime := time.Since(contentDownloadStart)
	if err != nil {
		fmt.Println(aurora.Red("Error reading response body:"), aurora.Red(err))
		return
	}

	fmt.Printf("%25s %-10s\n", aurora.Yellow("Content download time"), formatDuration(contentDownloadTime))
}

func calculateSize(resp *http.Response, client *http.Client) {
	resourceMap := make(map[string][]resource)
	baseURL, err := url.Parse(resp.Request.URL.String())
	if err != nil {
		fmt.Println(aurora.Red("Error parsing base URL:"), aurora.Red(err))
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(aurora.Red("Error reading response body:"), aurora.Red(err))
		return
	}

	// Add the page itself as a resource
	pageResource := resource{
		URL:  resp.Request.URL.String(),
		Size: int64(len(body)),
		Type: resp.Header.Get("Content-Type"),
	}
	resourceMap[pageResource.Type] = append(resourceMap[pageResource.Type], pageResource)

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		fmt.Println(aurora.Red("Error parsing HTML:"), aurora.Red(err))
		return
	}

	// Find links to other resources
	doc.Find("link[href], script[src], img[src]").Each(func(i int, s *goquery.Selection) {
		link, exists := s.Attr("href")
		if !exists {
			link, exists = s.Attr("src")
		}

		if exists {
			resource := fetchResource(link, baseURL, client)
			if resource != nil {
				resourceMap[resource.Type] = append(resourceMap[resource.Type], *resource)
			}
		}
	})

	// Print resource sizes
	var totalSize int64
	for resType, resources := range resourceMap {
		fmt.Println(aurora.Green("Type:"), aurora.Blue(resType))
		var typeTotalSize int64
		for _, resource := range resources {
			fmt.Println(aurora.Green(resource.URL), aurora.Blue(resource.Size))
			typeTotalSize += resource.Size
			totalSize += resource.Size
		}
		fmt.Println(aurora.Green("Total size for this type:"), aurora.Blue(typeTotalSize))
	}
	fmt.Println(aurora.Green("Total size for all resources:"), aurora.Blue(totalSize))
}

func fetchResource(link string, baseURL *url.URL, client *http.Client) *resource {
	resourceURL, err := url.Parse(link)
	if err != nil {
		fmt.Println(aurora.Red("Error parsing resource URL:"), aurora.Red(err))
		return nil
	}

	fullURL := baseURL.ResolveReference(resourceURL)
	req, err := http.NewRequest("GET", fullURL.String(), nil)
	if err != nil {
		fmt.Println(aurora.Red("Error creating request for resource:"), aurora.Red(err))
		return nil
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(aurora.Red("Error fetching resource:"), aurora.Red(err))
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(aurora.Red("Error reading resource body:"), aurora.Red(err))
		return nil
	}

	return &resource{
		URL:  fullURL.String(),
		Size: int64(len(body)),
		Type: resp.Header.Get("Content-Type"),
	}
}
