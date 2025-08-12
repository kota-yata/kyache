package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sync"

	"github.com/quic-go/quic-go/http3"
)

var nodeId = flag.String("nodeId", "unknown_node", "Name of the node")
var listenAddr = flag.String("listenAddr", ":8000", "Address to listen on")
var certFile = flag.String("cert", "", "TLS certificate file (required for HTTP/3)")
var keyFile = flag.String("key", "", "TLS private key file (required for HTTP/3)")

type requestInfo struct {
	RemoteAddr string
	PopCacheId string
	OriginId   string
}

func dumpRequestInfo(r *http.Request) requestInfo {
	return requestInfo{
		RemoteAddr: r.RemoteAddr,
		PopCacheId: r.Header.Get("X-NCDN-PoPCache-NodeId"),
		OriginId:   *nodeId,
	}
}

func serveIndexHTMLInternal(w http.ResponseWriter, r *http.Request) error {
	tmpl, err := template.New("index.html.gotmpl").ParseFiles("./templates/index.html.gotmpl")
	if err != nil {
		return fmt.Errorf("failed to parse index.html template: %w", err)
	}

	ri := dumpRequestInfo(r)

	var buf bytes.Buffer
	if err = tmpl.Execute(&buf, &ri); err != nil {
		return fmt.Errorf("failed to execute index.html template: %w", err)
	}

	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Cache-Control", "max-age=60, public")
	_, err = w.Write(buf.Bytes())
	if err != nil {
		log.Printf("Failed to write response: %v", err)
		return nil // since it is too late to recover
	}

	return nil
}

func serveIndexHTML(w http.ResponseWriter, r *http.Request) {
	err := serveIndexHTMLInternal(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func serveJsonInternal(w http.ResponseWriter, r *http.Request) error {
	ri := dumpRequestInfo(r)

	bs, err := json.MarshalIndent(ri, "", "  ")
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(bs)
	if err != nil {
		log.Printf("Failed to write response: %v", err)
		return nil // since it is too late to recover
	}

	return nil
}

func serveJson(w http.ResponseWriter, r *http.Request) {
	err := serveJsonInternal(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func main() {
	flag.Parse()

	fs := http.FileServer(http.Dir("./static"))

	mux := http.NewServeMux()
	mux.HandleFunc("/index.html", serveIndexHTML)
	mux.HandleFunc("/json", serveJson)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			log.Printf("Redirecting %s to /index.html", r.RemoteAddr)
			http.Redirect(w, r, "/index.html", http.StatusPermanentRedirect)
			return
		}

		fs.ServeHTTP(w, r)
	})

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if *certFile != "" && *keyFile != "" {
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
				switch r.URL.Path {
				case "/index.html":
					log.Printf("Serving index.html for %s", r.RemoteAddr)
					serveIndexHTMLInternal(w, r)
				case "/json":
					serveJsonInternal(w, r)
				default:
					fs.ServeHTTP(w, r)
				}
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
				fmt.Fprint(w, "oreno cache server!!!!!!")
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
