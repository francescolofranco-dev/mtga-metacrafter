# metacrafter

> "I have wildcards in MTG Arena — what should I craft?"

A small web app that ranks the cards most worth crafting in the current
Standard meta. It tells you, for each top card: how many copies to craft
(1–4), the wildcard rarity, and which competitive decks play it.

The dataset refreshes weekly.

## Why

Picking what to craft with limited wildcards is a guessing game for most
players. The information *exists* (in tournament results and meta reports)
but never in a single, easy-to-scan list. This is that list.

## How it works

- **Card data**: [Scryfall](https://scryfall.com/docs/api) bulk JSON
  (official, free).
- **Tournament data**: [MTGGoldfish](https://www.mtggoldfish.com/) recent
  tournament listings per format. For each format we take the most recent
  non-League events (last 30 days) and pull their top deck standings.
  Scraped daily at a polite cadence (1 req/sec, descriptive `User-Agent`).
  If you maintain MTGGoldfish and want this removed, open an issue.
- **Scoring**:
  `score(card) = Σ over decks (copies_in_deck × tier_weight)`, where
  `tier_weight = stars + 1` (so a 3-star Pro Tour deck counts 4× a base
  challenge deck). MTGO leagues are excluded entirely.
- **Recommended copies**: the max copy count seen for that card in any
  single deck, clamped to 1–4.
- **Cross-format bonus**: every card row shows other formats where the
  same card is also in the top-30 (e.g. a Standard card flagged "+Pioneer").

## Architecture

A single Go binary. It serves the site via `html/template` + HTMX and
runs the scraper on an internal daily schedule, holding the result in
memory plus a JSON snapshot on disk. Deployed on Fly.io.

## Known limitations

- "Most played in top tournaments" isn't always "best to craft" for a
  budget player. A budget mode (weight by inverse rarity) is on the roadmap.
- Standard rotation (every September) stales the data instantly; the
  page surfaces a "data as of …" banner.
- No MTGA collection sync yet — deferred.

## Development

```
go test ./...
go run ./cmd/metacrafter
# open http://localhost:8080
```

Environment variables:

| name              | default          | purpose                              |
|-------------------|------------------|--------------------------------------|
| `LISTEN_ADDR`     | `:8080`          | HTTP listen address                  |
| `DATA_DIR`        | `./data`         | snapshot location                    |
| `ADMIN_TOKEN`     | (unset)          | secret required for `/admin/refresh` |
| `SEED_PATH`       | `./seed.json`    | initial dataset for empty `DATA_DIR` |
| `REFRESH_PERIOD`  | `24h` (daily)    | scrape interval                      |
| `FORMATS`         | `standard,pioneer` | comma-separated slugs to rank      |
| `LOG_LEVEL`       | `info`           | `debug`, `info`, `warn`, `error`     |

Supported format slugs: `standard`, `pioneer`, `modern`, `pauper`, `legacy`.

## License

MIT.
