package main

import (
	"bytes"
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/logrusorgru/aurora"
)

type Resource struct {
	Url  string
	Size int64
	Type string
}

var appVersion = "0.1.3"

func main() {
	// Parse command line arguments
	urlArg := flag.String("url", "", "URL to send the HTTP HEAD request to")
	sizeArg := flag.Bool("size", false, "Calculate size of resources")
	verArg := flag.Bool("v", false, "Print version information")
	flag.Parse()

	if *verArg {
		fmt.Printf(aurora.Sprintf(aurora.Green("headview v%s\n"), aurora.Yellow(appVersion)))
		return
	}

	if *urlArg == "" {
		fmt.Println(aurora.Green("Please provide a URL using the -url flag."))
		return
	}

	client := createHttpClient()

	if *sizeArg {
		performGetSize(client, *urlArg)
	} else {
		performGetRequest(client, *urlArg)
	}
}

func createHttpClient() *http.Client {
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

func performGetRequest(client *http.Client, urlArg string) {
	req, err := http.NewRequest("HEAD", urlArg, nil)
	if err != nil {
		fmt.Println(aurora.Green("Error creating request:"), aurora.Blue(err))
		return
	}

	trace := createHttpTrace()
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(aurora.Red("Error sending request:"), aurora.Red(err))
	} else {
		defer resp.Body.Close()
		printResponse(start, resp)
	}
}

func createHttpTrace() *httptrace.ClientTrace {
	var start, connect, dns, tlsHandshake time.Time
	return &httptrace.ClientTrace{
		DNSStart: func(_ httptrace.DNSStartInfo) { dns = time.Now() },
		DNSDone: func(_ httptrace.DNSDoneInfo) {
			fmt.Printf("DNS lookup duration: %v\n", time.Since(dns))
		},
		ConnectStart: func(_, _ string) { connect = time.Now() },
		ConnectDone: func(_, _ string, err error) {
			if err != nil {
				fmt.Printf("Error during connection: %v\n", err)
				return
			}
			fmt.Printf("TCP connection duration: %v\n", time.Since(connect))
		},
		TLSHandshakeStart: func() { tlsHandshake = time.Now() },
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			fmt.Printf("TLS handshake duration: %v\n", time.Since(tlsHandshake))
		},
		GotFirstResponseByte: func() {
			fmt.Printf("Time to first byte: %v\n", time.Since(start))
		},
	}
}

func printResponse(start time.Time, resp *http.Response) {
	fmt.Println(aurora.Green("Total request duration:"), aurora.Blue(time.Since(start)))
	fmt.Println(aurora.Green("Response status:"), aurora.Blue(resp.Status))
	if lastMod, ok := resp.Header["Last-Modified"]; ok {
		fmt.Println(aurora.Green("Last Modified:"), aurora.Blue(lastMod))
	} else {
		fmt.Println(aurora.Green("Last Modified header not present"))
	}
	fmt.Println()
	fmt.Println(aurora.Green("Response headers:"))
	for key, values := range resp.Header {
		for _, value := range values {
			fmt.Println(aurora.Green(key+": "), aurora.Blue(value))
		}
	}
}

func calculateSize(resp *http.Response, client *http.Client) {
	resourceMap := make(map[string][]Resource)
	baseUrl, err := url.Parse(resp.Request.URL.String())
	if err != nil {
		fmt.Println(aurora.Red("Error parsing base URL:"), aurora.Red(err))
		return
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(aurora.Red("Error reading response body:"), aurora.Red(err))
		return
	}

	// Add the page itself as a resource
	pageResource := Resource{
		Url:  resp.Request.URL.String(),
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
			resource := fetchResource(link, baseUrl, client)
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
			fmt.Println(aurora.Green(resource.Url), aurora.Blue(resource.Size))
			typeTotalSize += resource.Size
			totalSize += resource.Size
		}
		fmt.Println(aurora.Green("Total size for this type:"), aurora.Blue(typeTotalSize))
	}
	fmt.Println(aurora.Green("Total size for all resources:"), aurora.Blue(totalSize))
}

func fetchResource(link string, baseUrl *url.URL, client *http.Client) *Resource {
	resourceUrl, err := url.Parse(link)
	if err != nil {
		fmt.Println(aurora.Red("Error parsing resource URL:"), aurora.Red(err))
		return nil
	}

	fullUrl := baseUrl.ResolveReference(resourceUrl)
	req, err := http.NewRequest("GET", fullUrl.String(), nil)
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

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(aurora.Red("Error reading resource body:"), aurora.Red(err))
		return nil
	}

	return &Resource{
		Url:  fullUrl.String(),
		Size: int64(len(body)),
		Type: resp.Header.Get("Content-Type"),
	}
}
