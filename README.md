<div align="center">

<img src="https://img.shields.io/github/license/Easonliuliang/purify?style=flat-square&color=22C55E" alt="License" />
<img src="https://img.shields.io/github/stars/Easonliuliang/purify?style=flat-square&color=22C55E" alt="Stars" />
<img src="https://img.shields.io/github/v/release/Easonliuliang/purify?style=flat-square&color=22C55E" alt="Release" />

# Purify

**The single-binary alternative to Firecrawl.**<br/>
Web scraping for AI agents — zero dependencies, 99% token savings.

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
| MCP server | **Built-in** | Community | None |
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
| BBC News homepage | 65,804 | 6,160 | **90.6%** | 0.6s |
| Next.js blog (React SPA) | 87,231 | 4,271 | **95.1%** | 5.0s |
| GitHub repo page | 103,954 | 1,391 | **98.7%** | 2.6s |
| Wikipedia (Rust) | 312,973 | 77,202 | **75.3%** | 2.7s |
| sspai.com | 32,895 | 187 | **99.4%** | 1.2s |
| Xiaohongshu (RedNote) | 158,742 | 353 | **99.8%** | 1.0s |

> paulgraham.com (11.3% savings) is excluded because the page is already minimal — almost pure text with nothing to remove.

### JavaScript-heavy and login-walled sites

Purify uses a real headless Chrome with stealth mode. It renders JavaScript, handles SPAs, and works with sites that block traditional scrapers.

Tested on: **Xiaohongshu**, **Baidu Baike**, **GitHub**, **Next.js apps**, and more.

## Use cases

- **AI agents** — Give your agent web access via MCP or REST API
- **RAG pipelines** — Scrape docs, get clean Markdown, embed into your vector DB
- **Trading bots** — Scrape prediction markets and news with sub-500ms latency
- **Research assistants** — Read and summarize any web page

## MCP server

Purify includes a built-in MCP server. Connect Claude Desktop, Cursor, or any MCP-compatible client with one config file:

```json
{
  "mcpServers": {
    "purify": {
      "command": "npx",
      "args": ["-y", "purify-mcp"],
      "env": {
        "PURIFY_API_KEY": "your-api-key"
      }
    }
  }
}
```

For self-hosted instances:

```json
"PURIFY_BASE_URL": "http://localhost:8080"
```

Then ask Claude: *"Scrape https://paulgraham.com/greatwork.html and summarize it."*

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
  "content": "# Article Title\n\nClean markdown content...",
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

### Structured extraction

Send a JSON schema, get structured data back. Uses your own LLM key (BYOK).

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
Client → HTTP API (Gin) → Headless Chrome (Rod) → Raw HTML
                                                      ↓
                                              Readability extraction
                                                      ↓
                                              Markdown conversion
                                                      ↓
                                              Clean content + metadata
```

- **Scraper** — Rod-based headless Chrome with page pool, resource blocking (images/CSS/fonts), and stealth mode
- **Cleaner** — Two-stage pipeline: Mozilla Readability for content extraction, html-to-markdown for format conversion
- **API** — Gin with API key auth, per-key rate limiting, health checks, and graceful shutdown

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
