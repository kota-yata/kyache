package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/quic-go/quic-go/http3"
)

var (
	url = flag.String("url", "https://localhost:8889/", "URL to request")
)

func main() {
	flag.Parse()

	// Create HTTP/3 client
	client := &http.Client{
		Transport: &http3.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	// Make request
	resp, err := client.Get(*url)
	if err != nil {
		log.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Failed to read response: %v", err)
	}

	// Display results
	fmt.Printf("Status: %s\n", resp.Status)
	fmt.Printf("Protocol: %s\n", resp.Proto)
	fmt.Printf("Headers:\n")
	for k, v := range resp.Header {
		fmt.Printf("  %s: %v\n", k, v)
	}
	fmt.Printf("\nBody:\n%s\n", string(body))
}
