# Log Me In - Local Auth Helper

A lightweight HTTP server that simplifies OAuth authentication flow for CMD SaaS APIs.

## What It Does

This tool runs a local web server that:
1. Orchestrates the OAuth flow with your browser
2. Captures the `session_id` after successful authentication
3. Displays it for easy copying
4. Works with any CMD SaaS environment (dev, staging, prod)

## Why This Exists

The CMD SaaS authentication flow requires:
- OAuth redirect to Cognito Hosted UI
- Callback handling after authentication
- Session ID extraction

Instead of running the full Drive UI Next.js app just to get a session ID, this tool provides a minimal interface for authentication.

## Usage

### Start the Server

**Default port (8080):**
```bash
cd app-plane/drive/log-me-in
go run main.go
```

**Custom port (e.g., 3000 for CORS compatibility):**
```bash
go run main.go -port=3000
```

**Or run the compiled binary:**
```bash
./log-me-in -port=3000
```

### Authenticate

1. Open http://localhost:8080 in your browser
2. Enter the API Gateway URL (e.g., `https://api.acme-corp.feature-856386.saas.cmddev.thermofisher.com`)
3. Click "Login" → redirects to Cognito
4. Enter your credentials at Cognito Hosted UI
5. After successful login, you're redirected back
6. Session ID is displayed → click "Copy to Clipboard"
7. The URL is automatically saved to "Recent URLs" for quick access next time

### Use the Session ID

**Query Drive Service (list root folder):**
```bash
curl -H "Cookie: session_id=YOUR_SESSION_ID" \
  -H "Origin: http://localhost:3000" \
  https://api.acme-corp.dev-gummi.saas.cmddev.thermofisher.com/api/v1/drive/internal/folders
```

**With environment variable:**
```bash
export SESSION_ID="YOUR_SESSION_ID"
curl -H "Cookie: session_id=$SESSION_ID" \
  -H "Origin: http://localhost:3000" \
  https://api.acme-corp.dev-gummi.saas.cmddev.thermofisher.com/api/v1/drive/internal/folders
```

**Other Drive API endpoints:**
```bash
# Get user profile (auth endpoint)
curl -H "Cookie: session_id=$SESSION_ID" \
  -H "Origin: http://localhost:3000" \
  https://api.acme-corp.dev-gummi.saas.cmddev.thermofisher.com/user/profile

# Create a folder
curl -X POST \
  -H "Cookie: session_id=$SESSION_ID" \
  -H "Origin: http://localhost:3000" \
  -H "Content-Type: application/json" \
  -d '{"name":"My Folder","parent_id":"root"}' \
  https://api.acme-corp.dev-gummi.saas.cmddev.thermofisher.com/api/v1/drive/internal/folders

# Get file by ID
curl -H "Cookie: session_id=$SESSION_ID" \
  -H "Origin: http://localhost:3000" \
  https://api.acme-corp.dev-gummi.saas.cmddev.thermofisher.com/api/v1/drive/internal/files/{fileId}
```

**Note:** The `-H "Origin: http://localhost:3000"` header is required because the Lambda validates CORS origins.

## How It Works

```
1. User visits http://localhost:8080
2. Enters API Gateway URL
3. Clicks "Login" → server redirects to:
   https://api.{tenant}.{env}.saas.cmddev.thermofisher.com/user/login?redirect_uri=http://localhost:8080/callback
4. API Gateway redirects to Cognito Hosted UI
5. User authenticates
6. Cognito calls API Gateway callback
7. API Gateway redirects to http://localhost:8080/callback?session_id=...
8. Local server captures session_id and displays it
```

## Features

- ✅ No dependencies (uses Go stdlib + embedded static files)
- ✅ Works with any CMD SaaS environment
- ✅ Dynamic presets - automatically remembers your last 5 URLs
- ✅ Clean, simple UI (Pico CSS)
- ✅ Copy session ID to clipboard
- ✅ Example curl command with your actual URL

## Configuration

The server accepts these flags:

- `-port`: Port to run on (default: 8080)

## Notes

- The session ID is stored in server memory only
- Recent URLs are stored in browser localStorage (persists across sessions)
- Maximum of 5 recent URLs are kept (oldest is auto-removed when full)
- Click "Logout" to clear the current session (keeps recent URLs)
- The server keeps running - use Ctrl+C to stop
- Session IDs expire based on Cognito token lifetime (typically 1 hour)

## Troubleshooting

**"Missing required redirect_uri query parameter"**
- Make sure you entered a valid API Gateway URL
- The URL should match the pattern: `https://api.{tenant}.{env}.saas.cmddev.thermofisher.com`

**"Missing session_id in callback"**
- This usually means authentication failed at Cognito
- Check your username/password
- Check if your user exists in the tenant's Cognito User Pool

**Browser doesn't redirect back**
- Make sure the server is still running
- Check if the port matches (default: 8080)
- Look at server logs for errors
