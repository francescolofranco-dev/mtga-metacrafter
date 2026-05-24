# MTGA MetaCrafter

> "I have wildcards in MTG Arena — what should I craft?"

A small web app that ranks the cards most worth crafting per format, based on
how often they appear in recent tournament top finishes. Each row shows
recommended copies (1–4), wildcard rarity, the archetypes that play it, and —
for Standard — how close it is to rotation.

**Live**: https://mtga-metacrafter.fly.dev/

**Source**: this repo. MIT-licensed, contributions welcome.

The dataset refreshes daily.

## Why

Picking what to craft with limited wildcards is a guessing game. The data
*exists* in tournament results — but never in a single, easy-to-scan,
"what should I actually craft this week?" list. This is that list.

## How it works

- **Card data**: [Scryfall](https://scryfall.com/docs/api) bulk JSON and
  `/sets` (official, free).
- **Tournament data**: [MTGGoldfish](https://www.mtggoldfish.com/) recent
  tournament listings per format. We walk the most recent pages, drop MTGO
  Leagues entirely, and pull the top deck standings from each remaining
  event. Scraped daily at a polite cadence (1 req/sec, descriptive UA). If
  you maintain MTGGoldfish and want this removed, open an issue.
- **Scoring**:
  `score(card) = Σ over decks (copies_in_deck × tier_weight)`,
  where `tier_weight = stars + 1` (so a 3-star event counts 4× a base
  challenge). Cards must appear in ≥ 2 distinct decks to make the list.
- **Rotation penalty (Standard only)**: cards close to rotating out of
  Standard get their score multiplied down: ≥ 180 days left → 1.0,
  90d → 0.5, 30d → 0.2, ≤ 7d → 0.05. Each row carries a "rotates in ~Nd"
  badge when applicable.
- **Recommended copies**: highest copy count seen in any single deck,
  clamped to 1–4. (Singleton formats like Commander/Brawl naturally
  resolve to 1.)
- **Cross-format bonus**: each row shows the other configured formats
  where the same card is also in that format's top-30 (e.g. a Standard
  card flagged "+Pioneer").

## Supported formats

MTGGoldfish's tournament URLs cover: `standard`, `pioneer`, `explorer`,
`alchemy`, `historic`, `timeless`, `modern`, `pauper`, `legacy`, `vintage`,
`commander`, `brawl`. Each instance is configured with the `FORMATS` env
var — defaults to `standard,pioneer`.

Data volume varies hugely between formats; small / casual formats may
yield very thin rankings.

## Architecture

A single Go binary. Serves `html/template` + HTMX on top of a per-format
in-memory dataset that's refreshed on an internal daily schedule and
persisted to a JSON snapshot for cold-start recovery. Deployed on
Fly.io free tier.

## Known limitations

- "Most played in top tournaments" isn't always "best to craft" for a
  budget player. A budget-mode toggle is on the roadmap.
- Rotation dates are *estimated* (`set_release + 3 years`) — close enough
  for the multiplier, but not authoritative.
- MTGO leagues are excluded as low-stakes 5-0 dumps; if a format only
  shows league data (rare formats), its ranking will be empty.
- No MTGA collection sync yet — deferred.

## Development

```
go test ./...
go run ./cmd/mtga-metacrafter
# open http://localhost:8080
```

Environment variables:

| name              | default              | purpose                                |
|-------------------|----------------------|----------------------------------------|
| `LISTEN_ADDR`     | `:8080`              | HTTP listen address                    |
| `DATA_DIR`        | `./data`             | snapshot location                      |
| `ADMIN_TOKEN`     | (unset)              | secret required for `/admin/refresh`   |
| `SEED_PATH`       | `./seed.json`        | initial dataset for empty `DATA_DIR`   |
| `REFRESH_PERIOD`  | `24h` (daily)        | scrape interval                        |
| `FORMATS`         | `standard,pioneer`   | comma-separated slugs to rank          |
| `LOG_LEVEL`       | `info`               | `debug`, `info`, `warn`, `error`       |

Supported format slugs: `standard`, `pioneer`, `explorer`, `alchemy`,
`historic`, `timeless`, `modern`, `pauper`, `legacy`, `vintage`,
`commander`, `brawl`.

## License

MIT.
