# Gemini Web 2 API

[English](./README.md) | [中文](./ZH_CN.md)

`Gemini Web 2 API` is a lightweight Go proxy that exposes OpenAI-compatible `/v1/chat/completions` and `/v1/models` endpoints while forwarding requests to the Gemini web experience. It supports streaming responses, session continuity, token refresh, and a small telemetry endpoint for the built-in WebUI.

### Highlights

- OpenAI-compatible chat and model endpoints
- Streaming SSE responses
- Session reuse with `X-Session-ID` or `conversation_id`
- Optional proxy and cookie-based token refresh
- Telemetry endpoint at `/api/telemetry`
- Embedded dashboard at `/` and manual page at `/help`
- WebUI defaults to English and can switch to Chinese

### Quick Start

1. Enter the project directory:

```bash
cd e:/Project/AI/All2API/geminiweb2api
```

2. Install dependencies and run:

```bash
go mod tidy
go run ./cmd/geminiweb2api
```

Project layout now uses folders by responsibility:

- `cmd/geminiweb2api`: application entrypoint
- `internal/server`: HTTP server assembly and routing
- `internal/gemini`: Gemini request/response logic
- `internal/config`, `internal/logging`, `internal/httpclient`, `internal/token`, `internal/metrics`: infrastructure modules
- `internal/web`: embedded dashboard and help pages

3. Default listen address:

- `http://127.0.0.1:8080`

4. Verify the service:

```bash
curl -s "http://127.0.0.1:8080/api/telemetry"
curl -s "http://127.0.0.1:8080/v1/models" \
  -H "Authorization: Bearer your-api-key-here"
```

### Docker

Build the image:

```bash
docker build -t geminiweb2api:latest .
```

Run with a local `config.json` mounted into the container:

```bash
docker run -d \
  --name geminiweb2api \
  -p 8080:8080 \
  -v $(pwd)/config.json:/app/config.json \
  geminiweb2api:latest
```

Windows PowerShell example:

```powershell
docker run -d `
  --name geminiweb2api `
  -p 8080:8080 `
  -v ${PWD}\config.json:/app/config.json `
  geminiweb2api:latest
```

If you need outbound proxy support inside the container, pass the standard proxy environment variables:

```bash
docker run -d \
  --name geminiweb2api \
  -p 8080:8080 \
  -v $(pwd)/config.json:/app/config.json \
  -e HTTP_PROXY=http://host.docker.internal:7890 \
  -e HTTPS_PROXY=http://host.docker.internal:7890 \
  geminiweb2api:latest
```

Docker Compose:

```bash
cp config.json.example config.json
docker compose up -d --build
```

### Configuration

Use `config.json` in the project root. You can start from `config.json.example`:

```json
{
  "api_key": "your-api-key-here",
  "token": "",
  "cookies": "",
  "tokens": null,
  "proxy": "",
  "gemini_url": "",
  "gemini_home_url": "",
  "port": 8080,
  "log_file": "",
  "log_level": "info",
  "note": [
    "Auto-generated config"
  ]
}
```

### Config Fields

- `api_key`
  Protects this local service. Clients must send `Authorization: Bearer <api_key>`.
- `token`
  Manual `SNlM0e` fallback token for Gemini web when automatic fetch fails.
- `cookies`
  Gemini web cookie string. Recommended when anonymous access is unstable or the environment requires sign-in state.
- `tokens`
  Reserved field. Currently unused.
- `proxy`
  Explicit proxy such as `http://127.0.0.1:7890`. The app also respects `HTTP_PROXY`, `HTTPS_PROXY`, and `ALL_PROXY`.
- `gemini_url`
  Override for the Gemini generation endpoint in reverse-proxy setups.
- `gemini_home_url`
  Override for the Gemini homepage used for token discovery.
- `port`
  Local listen port, default `8080`.
- `log_file`
  Log output path. Empty means stdout.
- `log_level`
  `debug`, `info`, `warn`, or `error`.
- `note`
  Free-form note strings surfaced by `/api/telemetry` and the WebUI.

### Hot Reload

The process checks `config.json` every 5 seconds and reloads it automatically when the file changes. You do not need to restart the service after editing the config.

### Token and Cookies

#### Automatic mode

- The service tries to fetch the anonymous Gemini token (`SNlM0e`) from the Gemini homepage by default.
- You can often run without `cookies`, but adding them improves stability.

#### Manual token mode

When automatic fetch fails:

1. Open `https://gemini.google.com/` or your configured `gemini_home_url`.
2. Open browser devtools.
3. Search for `SNlM0e` in `Elements`, `Sources`, or `Network`.
4. Copy the value after `"SNlM0e":"..."`.
5. Put it into `config.json`:

```json
{
  "token": "SNlM0e-example-value"
}
```

#### Cookies

If your environment needs a logged-in browser session or you see protection pages:

1. Open Gemini in a working browser session.
2. Open devtools and find the cookie store.
3. Copy the full cookie string:

```text
SID=...; APISID=...; SAPISID=...; ...
```

4. Put it into `config.json`:

```json
{
  "cookies": "SID=...; APISID=...; SAPISID=...; ..."
}
```

Do not commit real cookies to a public repository.

### Usage Examples

#### Health check

```bash
curl -s "http://127.0.0.1:8080/api/telemetry"
```

#### Model list

```bash
curl -s "http://127.0.0.1:8080/v1/models" \
  -H "Authorization: Bearer your-api-key-here"
```

#### Non-stream chat

```bash
curl -s "http://127.0.0.1:8080/v1/chat/completions" \
  -H "Authorization: Bearer your-api-key-here" \
  -H "Content-Type: application/json" \
  -H "X-Session-ID: demo-session" \
  -d '{
    "model": "gemini-3-flash",
    "stream": false,
    "messages": [
      {"role": "system", "content": "You are a helpful assistant"},
      {"role": "user", "content": "Please introduce yourself."}
    ]
  }'
```

#### Streaming SSE

```bash
curl -N "http://127.0.0.1:8080/v1/chat/completions" \
  -H "Authorization: Bearer your-api-key-here" \
  -H "Content-Type: application/json" \
  -H "X-Session-ID: demo-session" \
  -d '{
    "model": "gemini-3-flash",
    "stream": true,
    "messages": [
      {"role": "user", "content": "Introduce yourself in three sentences."}
    ]
  }'
```

### Session Continuity

- Keep `X-Session-ID` stable for the same user or conversation.
- The service will reuse Gemini-side session context for better multi-turn continuity.

### Troubleshooting

- Gemini access fails or times out:
  Check `proxy` first, then system proxy environment variables.
- HTML, captcha, or protection pages appear:
  Try cookies, change IP/proxy, and enable `debug` logging.
- Conversation continuity is broken:
  Reuse the same `X-Session-ID`.
- Automatic token fetch fails:
  Manually set `token`, or verify that `cookies` are valid and not expired.

### WebUI

- `GET /` renders the dashboard
- `GET /help` renders the manual page
- Theme and language preferences are stored in local storage
- Default language is English, with a Chinese switch available in the UI
