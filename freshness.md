## Freshness in RFC9111
Freshness in RFC9111 is determined as follows:
```
is_fresh = current_age < freshness_lifetime
```

### current_age
current_age is calculated as follows:
```
current_age = initial_age + (now - stored_time)
```

initial_age is parsed `Age` header value if present. stored_time is the time when the content is stored in this cache server.

### freshness_lifetime
freshness_lifetime is determined with following priority:
- Cache-Control: max-age
  - If max-age directive is present, it will be the freshness_lifetime
- Expires - Date
  - If max-age directive is not present, Expire - Date will be the freshness_lifetime
  - This applies only when both Expire and Date are present. Otherwise freshness_lifetime will be 0

### heuristic freshness
When explicit freshness is not present, a heuristic freshness_lifetime can be used for responses whose status codes are heuristically cacheable and for responses marked explicitly cacheable with `Cache-Control: public`.

This cache uses `10% * (Date - Last-Modified)` as the heuristic freshness_lifetime when both `Date` and `Last-Modified` are valid HTTP dates. If `Cache-Control: max-age`, `Cache-Control: s-maxage`, `CDN-Cache-Control: max-age`, or `Expires` is present, heuristic freshness is not used.
