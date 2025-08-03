Terminology
- Original request: the request that caused the cached response to be stored
- Incoming request: the request of which cache key matches the original request

## Vary response handling
When response with Vary header arrives, this server stores if:
- Vary field is not "*"

When the incoming request arrives this server responds with the stored response if:
- every header field in the original request that the stored response's Vary header specifies match corresponding field of the request header

For example, if a stored response has Vary header with "accept-encoding, accept-language" as its field, a request must have exactly the same value of Accept-Encoding and Accept-Language header as the original request.
