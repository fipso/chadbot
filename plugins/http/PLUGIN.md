# HTTP Plugin

Make HTTP requests to allowed URLs. URLs must match configured regex patterns for security.

## Skills

### http_request

Make an HTTP request to an allowed URL.

**Parameters:**
- `url` (required): The URL to request - must match one of the allowed patterns
- `method`: HTTP method (GET, POST, PUT, PATCH, DELETE, HEAD). Default: GET
- `body`: Request body for POST/PUT/PATCH requests
- `headers`: JSON object of headers, e.g., `{"Content-Type": "application/json", "Authorization": "Bearer token"}`

**Example:**
```
http_request url="https://api.example.com/data" method="POST" body='{"key": "value"}' headers='{"Content-Type": "application/json"}'
```

### http_list_patterns

List all currently configured allowed URL patterns.

## Configuration

### allowed_patterns

List of regex patterns. Only URLs matching at least one pattern will be allowed. Add patterns using the UI or via TOML config import.

**Example patterns:**
- `^https://api\.example\.com/.*`
- `^https://httpbin\.org/.*`
- `^https://jsonplaceholder\.typicode\.com/.*`

**Pattern tips:**
- Use `^` to anchor at the start
- Escape dots with `\.`
- Use `.*` to match any characters

### timeout_seconds

Request timeout in seconds. Default: 30

### max_response_size

Maximum response body size in bytes. Responses larger than this will be truncated. Default: 1048576 (1MB)

## Security

This plugin requires explicit URL allowlisting via regex patterns. If no patterns are configured, all requests will be rejected. This prevents unauthorized access to internal services or arbitrary URLs.
