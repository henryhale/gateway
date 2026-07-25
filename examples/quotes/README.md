# qoutes

A random quote generator that demonstrates [`gateway`](https://github.com/henryhale/gateway) fanning a single
standard request out across unrelated third-party HTTP APIs.

## Providers

| Provider | Endpoint |
| --- | --- |
| ZenQuotes | `https://zenquotes.io/api/random/` |
| Motivational Spark API | `https://motivational-spark-api.vercel.app/api/quotes/random` |
| Text Into Images | `https://textintoimages.com/random-quote/api/` |

Each provider has its own response schema; a `gw.Codec` per provider (see
[`providers`](./providers)) translates it into the standard `Quote` type
defined in [`domain/quote.go`](./domain/quote.go). Application code, including the HTTP
handler in [`main.go`](main.go), only ever sees `Quote`.

Requests are distributed round-robin across the three providers. If a
provider fails with a timeout, rate limit, or unavailability error, the
gateway retries and, if needed, falls back to a different provider.

## Run

```bash
cp .env.example .env

go run main.go
```

Then, in another terminal:

```bash
curl http://localhost:8080/quote
```

```json
{
	"text": "The most effective way to do it, is to do it.",
	"author": "Amelia Earhart",
	"source": "zen_quotes"
}
```

The `X-Quote-Provider` response header names which provider served the quote.
