<div align="center">

# Purify

**The single-binary alternative to Firecrawl.**<br/>
Web scraping for AI agents — zero dependencies, 99% token savings.

<img src="https://img.shields.io/github/license/Easonliuliang/purify?style=flat-square&color=22C55E" alt="License" />
<img src="https://img.shields.io/github/stars/Easonliuliang/purify?style=flat-square&color=22C55E" alt="Stars" />
<img src="https://img.shields.io/github/v/release/Easonliuliang/purify?style=flat-square&color=22C55E" alt="Release" />

[Get Free API Key](https://purify.verifly.pro) · [API Docs](#api) · [MCP Server](#mcp-server) · [Self-Host](#quick-start)

</div>

---

## Why Purify?

|  | Purify | Firecrawl | Jina Reader |
|---|---|---|---|
| Dependencies | **None** | Redis + Playwright + PostgreSQL | Firebase + proprietary |
| Deployment | `./purify` | `docker compose` (5 containers) | No self-host docs |
| Binary size | **~15 MB** | ~2 GB (Docker images) | N/A |
| Token savings | **93–99%** | ~70–80% | ~60–70% |
| License | **Apache 2.0** | AGPL-3.0 | Partial open source |
| MCP server | **Built-in (5 tools)** | Community | None |
| Price (50k req/mo) | **$29/mo** | $49/mo | $49/mo |

## Quick start

**Option A: Build from source**

```bash
git clone https://github.com/Easonliuliang/purify.git
cd purify && make build
PURIFY_AUTH_ENABLED=false ./bin/purify
```

**Option B: Docker**

```bash
docker compose up -d
```

**Option C: Hosted API (no setup)**

```bash
curl -s -X POST https://purify.verifly.pro/api/v1/scrape \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"url": "https://news.ycombinator.com"}' | jq .content
```

Get a free API key at [purify.verifly.pro](https://purify.verifly.pro) — 500 requests/month, no credit card.

## Token savings — real numbers

Measured with [tiktoken](https://github.com/openai/tiktoken) (GPT-4 tokenizer). Purify strips navigation, ads, scripts, and styling — your LLM only sees the content.

| Website | Raw HTML | After Purify | Savings | Latency |
|---|---|---|---|---|
| GitHub repo page | 99,181 | 1,370 | **98.6%** | 1.1s |
| New York Times | 103,744 | 2,130 | **98.0%** | 1.1s |
| Anthropic API Docs | 129,066 | 4,837 | **96.3%** | 1.8s |
| Next.js blog (React SPA) | 87,231 | 4,271 | **95.1%** | 5.0s |
| BBC News homepage | 97,540 | 6,969 | **92.9%** | 2.5s |
| arXiv paper (DeepSeek-R1) | 26,684 | 3,129 | **88.3%** | 0.5s |
| Wikipedia (LLM) | 245,276 | 76,325 | **68.9%** | 1.5s |
| Hacker News | 11,708 | 5,572 | **52.4%** | 0.4s |
| sspai.com | 32,895 | 187 | **99.4%** | 1.2s |
| Xiaohongshu (RedNote) | 158,742 | 353 | **99.8%** | 1.0s |

> Low-savings sites (Hacker News, paulgraham.com) are already minimal — almost pure text with no cruft to remove. That's a feature, not a bug.

### JavaScript-heavy and login-walled sites

Purify uses a real headless Chrome with stealth mode and Chrome TLS fingerprinting. It renders JavaScript, handles SPAs, and works with sites that block traditional scrapers — including Cloudflare-protected pages.

Tested on: **Xiaohongshu**, **GitHub**, **New York Times**, **Anthropic Docs**, **arXiv**, **BBC News**, **Hacker News**, **Next.js apps**, and more.

## Use cases

- **AI agents** — Give your agent web access via MCP or REST API
- **RAG pipelines** — Scrape docs, get clean Markdown, embed into your vector DB
- **Trading bots** — Scrape prediction markets and news with sub-500ms latency
- **Research assistants** — Read and summarize any web page

## MCP server

Purify includes a built-in MCP server with **5 tools**:

| Tool | Description |
|---|---|
| `scrape_url` | Scrape a single page, return clean content |
| `batch_scrape` | Scrape multiple URLs in parallel |
| `crawl_site` | Recursively crawl a website (BFS) |
| `map_site` | Discover all URLs on a site |
| `extract_data` | Extract structured data with LLM (BYOK) |

### Setup

Add to your Claude Desktop config (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "purify": {
      "command": "purify-mcp",
      "env": {
        "PURIFY_API_URL": "https://purify.verifly.pro",
        "PURIFY_API_KEY": "your-api-key"
      }
    }
  }
}
```

For self-hosted instances, set `PURIFY_API_URL` to `http://localhost:8080`.

Then ask Claude:
- *"Scrape https://paulgraham.com/greatwork.html and summarize it."*
- *"Crawl the Next.js docs site, max 20 pages."*
- *"Extract the product name and price from this page: ..."*

## API

### POST /api/v1/scrape

Scrape a single page and return cleaned content. Supports JSON response or SSE streaming.

```json
{
  "url": "https://example.com/article",
  "output_format": "markdown",
  "extract_mode": "readability"
}
```

| Parameter | Type | Default | Description |
|---|---|---|---|
| `url` | string | *required* | Target URL |
| `output_format` | string | `markdown` | `markdown`, `html`, `text`, or `markdown_citations` |
| `extract_mode` | string | `readability` | `readability`, `raw`, `pruning`, or `auto` |
| `timeout` | int | `30` | Timeout in seconds (1–120) |
| `stealth` | bool | `false` | Anti-detection mode |
| `headers` | object | — | Custom HTTP headers |
| `cookies` | array | — | Cookies to set before navigation |
| `actions` | array | — | Browser interactions (click, scroll, wait, etc.) |
| `include_tags` | array | — | CSS selectors to keep |
| `exclude_tags` | array | — | CSS selectors to remove |
| `css_selector` | string | — | Extract only matching elements |
| `max_age` | int | `0` | Cache max age in ms (0 = no cache) |

Response:

```json
{
  "success": true,
  "status_code": 200,
  "final_url": "https://example.com/article",
  "content": "# Article Title\n\nClean markdown content...",
  "metadata": {
    "title": "Article Title",
    "author": "Author Name",
    "language": "en",
    "source_url": "https://example.com/article",
    "fetch_method": "http"
  },
  "links": {
    "internal": [{"href": "/about", "text": "About"}],
    "external": [{"href": "https://github.com/...", "text": "GitHub"}]
  },
  "images": [{"src": "https://example.com/hero.jpg", "alt": "Hero"}],
  "tokens": {
    "original_estimate": 32895,
    "cleaned_estimate": 187,
    "savings_percent": 99.43
  },
  "timing": {
    "total_ms": 1172,
    "navigation_ms": 1162,
    "cleaning_ms": 9
  },
  "engine_used": "http"
}
```

#### SSE streaming

Add `Accept: text/event-stream` header to receive Server-Sent Events instead of JSON:

```bash
curl -X POST https://purify.verifly.pro/api/v1/scrape \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -H "Accept: text/event-stream" \
  -d '{"url": "https://example.com"}'
```

Events: `scrape.started` → `scrape.navigated` → `scrape.completed` (or `scrape.error`).

#### Citation format

Use `"output_format": "markdown_citations"` to convert inline links to academic-style references:

```markdown
See [Google][1] and [GitHub][2]

---
[1]: https://google.com
[2]: https://github.com
```

### POST /api/v1/batch/scrape

Scrape multiple URLs in parallel. Returns a job ID for async polling.

```json
{
  "urls": ["https://a.com", "https://b.com", "https://c.com"],
  "options": {"output_format": "markdown"},
  "webhook_url": "https://your-server.com/callback",
  "webhook_secret": "your-hmac-secret"
}
```

Poll status: `GET /api/v1/batch/:id`

### POST /api/v1/crawl

Recursively crawl a website starting from a URL.

```json
{
  "url": "https://docs.example.com",
  "max_depth": 3,
  "max_pages": 100,
  "scope": "subdomain",
  "webhook_url": "https://your-server.com/callback",
  "webhook_secret": "your-hmac-secret"
}
```

Poll status: `GET /api/v1/crawl/:id`

### POST /api/v1/map

Discover all URLs on a site without scraping content.

```json
{"url": "https://example.com"}
```

### POST /api/v1/extract

Structured data extraction using your own LLM key (BYOK).

```bash
curl -X POST https://purify.verifly.pro/api/v1/extract \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://example.com/product",
    "schema": {
      "name": "string",
      "price": "number",
      "features": ["string"]
    },
    "llm_api_key": "your-openai-key"
  }'
```

### Webhook callbacks

Batch and Crawl endpoints support webhook notifications. When a job completes, Purify sends a POST request to your `webhook_url` with HMAC-SHA256 signature in the `X-Purify-Signature` header.

Events: `batch.completed`, `crawl.page`, `crawl.completed`, `crawl.failed`

Verify the signature:
```
HMAC-SHA256(webhook_secret, request_body) == X-Purify-Signature (sha256=<hex>)
```

### GET /api/v1/health

Returns server status, uptime, and browser pool stats.

## Configuration

All configuration via environment variables:

| Variable | Default | Description |
|---|---|---|
| `PURIFY_HOST` | `0.0.0.0` | Listen address |
| `PURIFY_PORT` | `8080` | Listen port |
| `PURIFY_AUTH_ENABLED` | `true` | Enable API key authentication |
| `PURIFY_API_KEYS` | — | Comma-separated valid API keys |
| `PURIFY_MAX_PAGES` | `10` | Max concurrent browser tabs |
| `PURIFY_DEFAULT_TIMEOUT` | `30s` | Default scrape timeout |
| `PURIFY_RATE_RPS` | `5` | Rate limit (requests/sec/key) |
| `PURIFY_RATE_BURST` | `10` | Rate limit burst |
| `PURIFY_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `PURIFY_MULTI_ENGINE` | `true` | Enable multi-engine racing |
| `PURIFY_ESCALATION_DELAYS` | `0s,2s,5s` | Engine start delays (http, rod, rod-stealth) |

## Self-hosting

Purify is a single Go binary. No Docker required, no Redis, no database.

```bash
# Local development (no auth)
PURIFY_AUTH_ENABLED=false ./bin/purify

# Production (with API key)
PURIFY_API_KEYS=your-secret-key ./bin/purify
```

Runs on any $5/month VPS. No usage limits when self-hosted.

### System requirements

- Any Linux, macOS, or Windows machine
- ~15 MB disk space
- ~30 MB RAM idle

## Architecture

```
Client request
    ↓
HTTP API (Gin) — auth, rate limit, SSE support
    ↓
Scraper — actions, stealth, resource blocking
    ↓
Multi-engine Dispatcher (racing)
    ├── HTTP Engine (Chrome TLS fingerprint via utls)  ← fastest
    ├── Rod Engine (headless Chrome)
    └── Rod Stealth Engine (anti-detection)
    ↓
Domain Memory — remembers best engine per domain
    ↓
Cleaner pipeline
    ├── Readability / Pruning / Auto extraction
    ├── Markdown / HTML / Text / Citations output
    └── Links, images, OG metadata extraction
    ↓
Response — content + tokens + timing + cache
    ↓
Optional: Webhook callback (HMAC-SHA256 signed)
```

- **Multi-engine racing** — HTTP, Rod, and Rod-stealth engines race in parallel with staged escalation. First success wins. Domain memory skips the race on repeat visits.
- **Chrome TLS fingerprint** — HTTP engine uses [utls](https://github.com/refraction-networking/utls) to mimic Chrome's TLS ClientHello, bypassing basic bot detection without a browser.
- **Adaptive pool** — Browser page pool auto-scales based on memory pressure and load.
- **Dual extraction** — Readability + scoring-based pruning run concurrently; `auto` mode picks the better result.

## Pricing

| | Free | Pro |
|---|---|---|
| Price | $0/mo | $29/mo |
| Requests | 500/mo | 50,000/mo |
| Concurrent | 2 | 10 |
| MCP server | ✓ | ✓ |
| Structured extraction | ✓ | ✓ |

**[Get started free →](https://purify.verifly.pro)**

## Contributing

Contributions welcome. Please open an issue first to discuss what you'd like to change.

## License

[Apache 2.0](LICENSE) — use it however you want, commercially or otherwise. No AGPL restrictions.
