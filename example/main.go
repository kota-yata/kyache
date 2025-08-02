package main

import (
	"flag"
	"log"
	"net/http"
	"net/url"

	"github.com/kota-yata/kyache"
)

var (
	originURLStr = flag.String("originURL", "http://localhost:8000", "Origin server URL")
	listenAddr   = flag.String("listenAddr", ":8889", "Address to listen on")
)

func main() {
	flag.Parse()

	originURL, err := url.Parse(*originURLStr)
	if err != nil {
		log.Fatalf("Failed to parse origin URL %q: %v", *originURLStr, err)
	}

	cache := kyache.New(&kyache.Config{})
	handler := cache.Handler(originURL)

	log.Printf("Starting cache server on %s, proxying to %s", *listenAddr, *originURLStr)
	if err := http.ListenAndServe(*listenAddr, handler); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
