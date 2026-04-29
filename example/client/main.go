package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

var (
	url         = flag.String("url", "https://192.168.20.101:4443/", "URL to request")
	keepAlive   = flag.Duration("keepalive", 30*time.Second, "Keep-alive period for QUIC connection")
	idleTimeout = flag.Duration("idle", 60*time.Second, "Idle timeout for QUIC connection")
	requests    = flag.Int("requests", 3, "Number of requests to make")
	interval    = flag.Duration("interval", 10*time.Second, "Interval between requests")
)

func main() {
	flag.Parse()

	// Create HTTP/3 client with keep-alive configuration
	client := &http.Client{
		Transport: &http3.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
				NextProtos:         []string{"h3"},
			},
			QUICConfig: &quic.Config{
				KeepAlivePeriod: *keepAlive,
				MaxIdleTimeout:  *idleTimeout,
			},
		},
	}

	fmt.Printf("QUIC Client Configuration:\n")
	fmt.Printf("Keep-Alive Period: %v\n", *keepAlive)
	fmt.Printf("Idle Timeout: %v\n", *idleTimeout)
	fmt.Printf("Making %d requests with %v interval\n\n", *requests, *interval)

	// Make multiple requests to demonstrate keep-alive
	for i := 1; i <= *requests; i++ {
		fmt.Printf("=== Request %d/%d ===\n", i, *requests)
		start := time.Now()

		resp, err := client.Get(*url)
		if err != nil {
			log.Printf("Request %d failed: %v", i, err)
			continue
		}

		// Read response
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			log.Printf("Failed to read response %d: %v", i, err)
			continue
		}

		duration := time.Since(start)

		// Display results
		fmt.Printf("Status: %s\n", resp.Status)
		fmt.Printf("Protocol: %s\n", resp.Proto)
		fmt.Printf("Request Duration: %v\n", duration)
		fmt.Printf("Response Size: %d bytes\n", len(body))

		if i == 1 {
			fmt.Printf("Headers:\n")
			for k, v := range resp.Header {
				fmt.Printf("  %s: %v\n", k, v)
			}
			fmt.Printf("\nFirst Response Body:\n%s\n", string(body))
		}

		fmt.Println()

		// Wait before next request (except for the last one)
		if i < *requests {
			fmt.Printf("Waiting %v before next request...\n\n", *interval)
			time.Sleep(*interval)
		}
	}

	fmt.Printf("All requests completed. Connection should remain alive during the session.\n")
}
