# Kyache
RFC 9111 compliant HTTP shared cache library.

Visit https://kota-yata.github.io/cache-tests/ to see how compliant kyache is with RFC 9111

## Installation

```bash
go get github.com/kota-yata/kyache
```

## API Usage
### As HTTP Transport

Use Kyache as an `http.RoundTripper` to add caching to any HTTP client:

```go
package main

import (
    "net/http"
    "github.com/kota-yata/kyache"
)

func main() {
    cache := kyache.New(&kyache.Config{})
    
    client := &http.Client{
        Transport: cache,
    }
    
    resp, err := client.Get("https://api.example.com/data")
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()
}
```

### As HTTP Handler

Use Kyache as an HTTP handler for proxy/gateway scenarios:

```go
package main

import (
    "net/http"
    "net/url"
    "log"
    "github.com/kota-yata/kyache"
)

func main() {
    cache := kyache.New(&kyache.Config{})

    originURL, _ := url.Parse("http://localhost:8080")
    
    handler := cache.Handler(originURL)
    
    log.Println("Cache server running on :8889")
    http.ListenAndServe(":8889", handler)
}
```

### Custom Transport

You can provide a custom transport for advanced networking configurations:

```go
transport := &http.Transport{
    MaxIdleConns:       10,
    IdleConnTimeout:    30 * time.Second,
}

cache := kyache.New(&kyache.Config{
    Transport: transport,
})
```
