# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Log Me In** is a minimal local HTTP server for OAuth authentication flow with CMD SaaS APIs. It eliminates the need to run the full Drive UI Next.js app just to obtain a `session_id`.

The tool orchestrates the OAuth flow: user → local server → API Gateway → Cognito Hosted UI → callback → display session_id.

## Architecture

This is a **single-file Go application** (`main.go`) with embedded static assets (`static/index.html`).

### Core Design
- **No external dependencies**: Uses only Go stdlib (`net/http`, `embed`, `sync`)
- **Embedded static files**: Frontend HTML is embedded via `//go:embed static/*` directive
- **In-memory session storage**: Session IDs stored in `Server` struct with mutex protection
- **Port flexibility**: Configurable via `-port` flag (default: 8080)
- **Dynamic presets**: API URLs stored in browser localStorage, automatically populated after successful login (max 5, FIFO)

### HTTP Endpoints
- `GET /` → Serves embedded `static/index.html`
- `GET /login?api_url=...` → Redirects to API Gateway OAuth login
- `GET /callback?session_id=...` → Captures session_id from OAuth callback, stores it, redirects to index
- `GET /session` → Returns current session_id as JSON
- `GET /logout` → Clears stored session_id

### OAuth Flow
```
Browser → /login?api_url={gateway}
  ↓
302 to {gateway}/user/login?redirect_uri=http://localhost:{port}/callback
  ↓
{gateway} → 302 to Cognito Hosted UI
  ↓
User authenticates at Cognito
  ↓
Cognito → {gateway}/callback → 302 to http://localhost:{port}/callback?session_id=...
  ↓
Server stores session_id, redirects to /?success=true
```

## Development Commands

### Run the server
```bash
go run main.go              # Default port 8080
go run main.go -port=3000   # Custom port
```

### Build binary
```bash
go build -o log-me-in
./log-me-in -port=3000
```

### Test manually
1. Start server: `go run main.go`
2. Open http://localhost:8080
3. Enter API Gateway URL (e.g., `https://api.acme-corp.dev-gummi.saas.cmddev.thermofisher.com`)
4. Complete OAuth flow
5. Verify session_id appears in UI

## Key Constraints

### CORS Headers Required
When using the session_id with curl/API calls, **always include**:
```bash
-H "Origin: http://localhost:3000"
```
The Lambda validates CORS origins and will reject requests without this header.

### Port Alignment
If using port 3000 (for CORS compatibility with Drive UI), ensure:
- Server runs on port 3000: `go run main.go -port=3000`
- Origin header matches: `-H "Origin: http://localhost:3000"`

### Session Lifetime
Session IDs expire based on Cognito token lifetime (typically 1 hour). No refresh mechanism exists.

## Frontend localStorage Schema

The UI uses localStorage for persistence:

- **`api_url`** (string): Currently selected API URL, restored on page load
- **`api_url_presets`** (JSON array): List of recent URLs (max 5)
  - Format: `["https://api.tenant1.env1.saas...", "https://api.tenant2.env2.saas...", ...]`
  - FIFO: When full, oldest URL is removed when new one is added
  - URLs are deduplicated (no duplicates in list)
  - Displayed as `{tenant} ({env})` format (e.g., "acme-corp (feature-856386)")
  - Automatically populated after successful OAuth login
