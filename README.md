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
- **Meta data**: [MTGGoldfish Standard
  metagame](https://www.mtggoldfish.com/metagame/standard) — archetype
  metashare and representative decklists. Scraped weekly at a polite
  cadence (1 request/second, descriptive `User-Agent`). If you maintain
  MTGGoldfish and would like this removed, please open an issue and we'll
  take it down.
- **Scoring**:
  `score(card) = Σ (archetype_metashare_pct × inclusion_pct × avg_copies / 100)`,
  summed over the archetypes that play it. A 4-of with 100% inclusion in a
  20%-meta deck contributes 80 points; a 1-of with 30% inclusion in a 5% deck
  contributes 1.5.
- **Recommended copies**: the max average copies the card runs across
  archetypes with ≥ 3% metashare, rounded and clamped to 1–4.

## Architecture

A single Go binary. It serves the site via `html/template` + HTMX and
runs the scraper on an internal weekly schedule, storing the result in
memory plus a JSON snapshot on disk. Deployed on Fly.io.

## Known limitations

- "Most played" isn't always "best to craft" — a budget player may prefer
  cheaper, slightly-less-played cards. A budget mode is on the v2 roadmap.
- Standard rotation (every September) stales the data instantly; the
  page surfaces a "data as of …" banner.
- No cross-format bonus and no MTGA collection sync in v1 — both
  deferred.

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
| `REFRESH_PERIOD`  | `168h` (1 week)  | scrape interval                      |
| `LOG_LEVEL`       | `info`           | `debug`, `info`, `warn`, `error`     |

## License

MIT.
