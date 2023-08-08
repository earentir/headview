package main

import "time"

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

var appVersion = "0.1.17"
var timeStats timmings
