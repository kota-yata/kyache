package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"flag"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/kota-yata/kyache"
	"github.com/quic-go/quic-go/http3"
)

var (
	originURLStr = flag.String("originURL", "http://localhost:8000", "Origin server URL")
	listenAddr   = flag.String("listenAddr", ":8889", "Address to listen on")
	enableHTTP3  = flag.Bool("http3", false, "Enable HTTP/3 support")
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
		server := &http3.Server{
			Addr:    *listenAddr,
			Handler: handler,
			TLSConfig: &tls.Config{
				Certificates: generateSelfSignedCert(),
			},
		}
		log.Printf("Starting HTTP/3 cache server on %s, proxying to %s", *listenAddr, *originURLStr)
		if err := server.ListenAndServe(); err != nil {
			log.Fatalf("HTTP/3 server failed to start: %v", err)
		}
	} else {
		log.Printf("Starting cache server on %s, proxying to %s", *listenAddr, *originURLStr)
		if err := http.ListenAndServe(*listenAddr, handler); err != nil {
			log.Fatalf("Server failed to start: %v", err)
		}
	}
}

func generateSelfSignedCert() []tls.Certificate {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatalf("Failed to generate private key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		DNSNames:              []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		log.Fatalf("Failed to create certificate: %v", err)
	}

	cert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}

	return []tls.Certificate{cert}
}
