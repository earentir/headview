package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/PuerkitoBio/goquery"
	"github.com/logrusorgru/aurora"
)

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
