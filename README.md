# Purify

Web scraping & content cleaning API built for AI agents. Single binary, zero external dependencies.

Turn any web page into clean, LLM-ready Markdown — with up to **99% token savings**.

## Why Purify?

|  | Purify | Firecrawl | Jina Reader |
|---|---|---|---|
| Language | Go | TypeScript | TypeScript |
| Self-host deps | **None** | Redis + Playwright + PostgreSQL | Firebase + closed-source internal pkg |
| Deployment | `./purify` | docker-compose (multi-container) | No official self-host docs |
| License | **Apache 2.0** | AGPL-3.0 | Apache 2.0 |
| JS rendering | Yes (headless Chrome) | Yes | Yes |
| MCP support | Built-in (`purify-mcp`) | Separate repo | Separate repo |

## Token Savings

Purify strips navigation, ads, scripts, and styling — keeping only the content that matters to your LLM.

| Website | Raw HTML Tokens | After Purify | Savings | Time |
|---|---|---|---|---|
| Xiaohongshu (RedNote) post | 158,742 | 353 | **99.8%** | 1.0s |
| sspai.com (少数派) | 32,895 | 187 | **99.4%** | 1.2s |
| GitHub repo page | 103,954 | 1,391 | **98.7%** | 2.6s |
| Next.js 15.2 blog (React SPA) | 87,231 | 4,271 | **95.1%** | 5.0s |
| BBC News homepage | 65,804 | 6,160 | **90.6%** | 0.6s |
| Wikipedia (Rust) | 312,973 | 77,202 | **75.3%** | 2.7s |
| paulgraham.com | 26,204 | 23,241 | 11.3% | 2.1s |

> paulgraham.com scores low because the page is already minimal — almost pure text with no cruft to remove. That's a feature, not a bug.

### JS-Heavy & Login-Walled Sites

Purify uses a real headless Chrome with stealth mode — it renders JavaScript, handles SPAs, and works with sites that block traditional scrapers.

Tested successfully on: **Xiaohongshu (RedNote)**, **Baidu Baike**, **GitHub**, **Next.js apps**, and more. No cookies or browser extensions required — just pass the URL.

## Quick Start

### Build from source

```bash
git clone https://github.com/Easonliuliang/purify.git
cd purify
make build
```

### Run

```bash
# Minimal (no auth, for local use)
PURIFY_AUTH_ENABLED=false ./bin/purify

# Production (with API key)
PURIFY_API_KEYS=your-secret-key ./bin/purify
```

### Docker

```bash
docker compose up -d
```

### Test

```bash
curl -s -X POST http://localhost:8080/api/v1/scrape \
  -H "Content-Type: application/json" \
  -d '{"url": "https://paulgraham.com/greatwork.html"}' | jq .content
```

## API

### POST /api/v1/scrape

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
| `output_format` | string | `markdown` | `markdown`, `html`, or `text` |
| `extract_mode` | string | `readability` | `readability` (main content) or `raw` (full page) |
| `timeout` | int | `30` | Timeout in seconds (1–120) |
| `stealth` | bool | `false` | Anti-detection mode |

Response:

```json
{
  "success": true,
  "content": "# Article Title\n\nArticle content in markdown...",
  "metadata": {
    "title": "Article Title",
    "author": "Author Name",
    "source_url": "https://example.com/article"
  },
  "tokens": {
    "original_estimate": 32895,
    "cleaned_estimate": 187,
    "savings_percent": 99.43
  },
  "timing": {
    "total_ms": 1172,
    "navigation_ms": 1162,
    "cleaning_ms": 9
  }
}
```

### GET /api/v1/health

Returns server status, uptime, and browser pool stats.

## MCP Server

Purify includes a built-in MCP (Model Context Protocol) server, so AI IDEs like Claude Desktop and Cursor can call it directly.

### Build

```bash
make build-mcp
```

### Configure Claude Desktop

Add to `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "purify": {
      "command": "/path/to/purify-mcp",
      "env": {
        "PURIFY_API_URL": "http://localhost:8080",
        "PURIFY_API_KEY": "your-api-key"
      }
    }
  }
}
```

Then ask Claude: *"Help me scrape https://paulgraham.com/greatwork.html and summarize it"*

## Configuration

All configuration via environment variables:

| Variable | Default | Description |
|---|---|---|
| `PURIFY_HOST` | `0.0.0.0` | Listen address |
| `PURIFY_PORT` | `8080` | Listen port |
| `PURIFY_AUTH_ENABLED` | `true` | Enable API key auth |
| `PURIFY_API_KEYS` | — | Comma-separated API keys |
| `PURIFY_MAX_PAGES` | `10` | Max concurrent browser tabs |
| `PURIFY_DEFAULT_TIMEOUT` | `30s` | Default scrape timeout |
| `PURIFY_RATE_RPS` | `5` | Rate limit (requests/sec/key) |
| `PURIFY_RATE_BURST` | `10` | Rate limit burst |
| `PURIFY_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

## Architecture

```
Client → HTTP API (Gin) → Headless Chrome (Rod) → Raw HTML
                                                      ↓
                                              Readability extraction
                                                      ↓
                                              Markdown conversion
                                                      ↓
                                              Clean content + metadata
```

- **Scraper**: Rod-based headless Chrome with page pool, resource blocking (images/CSS/fonts), and stealth mode
- **Cleaner**: Two-stage pipeline — Mozilla Readability for content extraction, then html-to-markdown for format conversion
- **API**: Gin with API key auth, per-key rate limiting, health checks, and graceful shutdown

## License

[Apache License 2.0](LICENSE)
