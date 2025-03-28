package main

import (
	"flag"
	"fmt"
	"os"

	"headview/network"
	"headview/output"
)

const appVersion = "0.1.18"

func main() {
	// Check if URL is provided
	if len(os.Args) < 2 {
		fmt.Println("Please provide a URL as the first argument.")
		os.Exit(1)
	}

	// Get URL from the first argument
	urlArg := network.AddDefaultProtocol(os.Args[1])

	// Create a new flag set to parse the remaining arguments
	flags := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	// Define the rest of your flags
	headersArg := flags.Bool("headers", false, "Print headers")
	sizeArg := flags.Bool("size", false, "Calculate size of resources")
	verArg := flags.Bool("v", false, "Print version information")
	concurrentArg := flags.Int("concurrent", 5, "Max concurrent requests for size calculation")

	// Parse the remaining command line arguments
	if err := flags.Parse(os.Args[2:]); err != nil {
		fmt.Println("Error parsing flags:", err)
		os.Exit(1)
	}

	if *verArg {
		output.PrintVersion(appVersion)
		return
	}

	client := network.CreateHTTPClient()

	if *sizeArg {
		resources, err := network.PerformGetSize(client, urlArg, *concurrentArg)
		if err != nil {
			fmt.Println("Error calculating size:", err)
			return
		}
		output.PrintResourceSizes(resources)
	} else {
		timeStats, responses, err := network.PerformGetRequest(client, urlArg, *headersArg)
		if err != nil {
			fmt.Println("Error performing request:", err)
			return
		}

		// Print combined response info and connection timings for each request
		for i, resp := range responses {
			// Print response info
			output.PrintResponseInfo(resp, timeStats.CommonTimings[i].DNSLookupTime > 0, *headersArg)

			// Print connection timing for this specific request
			if i < len(timeStats.CommonTimings) {
				output.PrintConnectionTiming(i+1, timeStats.CommonTimings[i])
			}
		}

		// Only print combined timing stats at the end
		output.PrintCombinedTimingStats(timeStats)
	}
}
