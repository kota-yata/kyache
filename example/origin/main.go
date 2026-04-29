package main

import (
	"context"
	"crypto/tls"
	"flag"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/quic-go/quic-go/http3"
)

var listenAddr = flag.String("listenAddr", ":8000", "Address to listen on")
var enableHTTP3 = flag.Bool("http3", false, "Enable HTTP/3 support")
var certFile = flag.String("cert", "./certs/kcdn.kota-yata.com.pem", "TLS certificate file (required for HTTP/3)")
var keyFile = flag.String("key", "./certs/kcdn.kota-yata.com-key.pem", "TLS private key file (required for HTTP/3)")

func main() {
	flag.Parse()

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if *enableHTTP3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Printf("Starting HTTP/3 server on %s...\n", *listenAddr)

			cert, err := tls.LoadX509KeyPair(*certFile, *keyFile)
			if err != nil {
				log.Printf("Failed to load TLS certificate: %v", err)
				cancel()
				return
			}

			tlsConfig := &tls.Config{
				Certificates: []tls.Certificate{cert},
			}

			server := &http3.Server{
				Addr:      *listenAddr,
				Handler:   nil, // use default handler
				TLSConfig: tlsConfig,
			}

			server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				htmlBytes, err := os.ReadFile("./example/origin/index.html")
				if err != nil {
					http.Error(w, "File not found", http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set("Cache-Control", "max-age=60, public")
				w.Header().Set("Alt-Svc", `h3="kcdn.kota-yata.com:4443"`)
				w.Write(htmlBytes)
			})

			err = server.ListenAndServe()
			if err != nil {
				log.Printf("HTTP/3 server error: %v", err)
				cancel()
			}
		}()
	} else {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Printf("Starting HTTP/1.1 and HTTP/2 server on %s...\n", *listenAddr)

			err := http.ListenAndServe(*listenAddr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				log.Printf("serving")
				http.ServeFile(w, r, "index.html")
			}))

			if err != nil {
				log.Printf("HTTP/1.1 and HTTP/2 server error: %v", err)
				cancel()
			}
		}()
	}

	<-ctx.Done()
	log.Printf("Shutting down servers...")
	wg.Wait()
}
