package main

import (
	"crypto/tls"
	"flag"
	"log"
	"net"
	"net/http"
	"net/url"

	"github.com/kota-yata/kyache"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

var (
	originURLStr = flag.String("originURL", "https://localhost:8000", "Origin server URL")
	listenAddr   = flag.String("listenAddr", ":8889", "Address to listen on")
	enableHTTP3  = flag.Bool("http3", false, "Enable HTTP/3 support")
	certFile     = flag.String("cert", "./certs/kcdn.kota-yata.com.pem", "TLS certificate file (required for HTTP/3)")
	keyFile      = flag.String("key", "./certs/kcdn.kota-yata.com-key.pem", "TLS private key file (required for HTTP/3)")
)

func main() {
	flag.Parse()

	originURL, err := url.Parse(*originURLStr)
	if err != nil {
		log.Fatalf("Failed to parse origin URL %q: %v", *originURLStr, err)
	}

	config := &kyache.Config{
		EnableHTTP3: *enableHTTP3,
	}

	if *enableHTTP3 {
		config.TLSConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}

	cache := kyache.New(config)
	handler := cache.Handler(originURL)

	if *enableHTTP3 {
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{
			IP:   net.IPv4(219, 100, 95, 50),
			Port: 4443,
		})
		if err != nil {
			log.Printf("Error")
		}
		addr := conn.LocalAddr().(*net.UDPAddr)
		tr := quic.Transport{
			Conn:                  conn,
			ConnectionIDLength:    8,
			ConnectionIDGenerator: NewQLBCID(addr.IP, addr.Port),
		}
		cert, err := tls.LoadX509KeyPair(*certFile, *keyFile)
		if err != nil {
			log.Printf("Failed to load TLS certificate: %v", err)
			return
		}

		tlsConfig := http3.ConfigureTLSConfig(&tls.Config{Certificates: []tls.Certificate{cert}})
		server := &http3.Server{
			Addr:       *listenAddr,
			Handler:    handler,
			TLSConfig:  tlsConfig,
			QUICConfig: &quic.Config{},
		}
		log.Printf("Starting HTTP/3 cache server on %s, proxying to %s", *listenAddr, *originURLStr)
		ln, _ := tr.ListenEarly(tlsConfig, &quic.Config{})
		err = server.ServeListener(ln)
		if err != nil {
			log.Printf("error")
		}
		// print when received initial packet
		// if err := server.ListenAndServe(); err != nil {
		// 	log.Fatalf("HTTP/3 server failed to start: %v", err)
		// }
	} else {
		log.Printf("Starting cache server on %s, proxying to %s", *listenAddr, *originURLStr)
		if err := http.ListenAndServe(*listenAddr, handler); err != nil {
			log.Fatalf("Server failed to start: %v", err)
		}
	}
}
