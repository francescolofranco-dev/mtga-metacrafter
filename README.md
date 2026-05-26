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
- **Tournament data**: [MTGTop8](https://mtgtop8.com/) — the canonical
  paper-tournament archive. We use their "Large Events Last 2 Months"
  view (RCQs, Regional Championships, MTGO Showcase / Challenges, Pro
  Tours, etc.). Casual store events and 5-0 MTGO Leagues are filtered
  out at the source. Scraped daily at 1 req/sec with a descriptive UA.
- **Scoring (deck-similarity clustering, no archetype labels)**:

  ```
  Two deck lists belong to the same cluster if their unique mainboard
  card names overlap ≥ 75% (Jaccard).
  For each cluster C:
    avg_tier(C)      = mean event tier weight across cluster's decks
    size(C)          = √(decks_in_cluster)
    contribution(C)  = avg_copies × avg_tier(C) × size(C)

  score(card) = Σ contribution(C) over every cluster that plays it
  ```

  Why this shape: a card sitting in 5 distinct clusters can outscore a
  card stuck in 1 cluster, even a big one. That matches the real
  question — "if I craft this card, how many different decks does it
  unlock?". Cards must appear in **≥ 2 clusters** to make the list.

  **Tier weight per event** (derived from MTGTop8 event titles):
  Pro Tour / Worlds → 5, Regional Championship / MagicCon → 4,
  RCQ / Open / Team Series → 3, MTGO Showcase → 2.5, Store Champ → 2,
  MTGO Challenge → 1.5, anything else → 1, MTGO League → 0.5.
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

MTGTop8 covers paper-Magic formats: `standard`, `pioneer`, `modern`,
`pauper`, `legacy`, `vintage`. MTGA-only formats (Alchemy, Historic,
Timeless, Brawl) aren't run as paper tournaments and would need a
different source — out of scope for now. Each instance is configured
with the `FORMATS` env var; defaults to `standard,pioneer`.

## Architecture

A single Go binary. Serves `html/template` + HTMX on top of a per-format
in-memory dataset that's refreshed on an internal daily schedule and
persisted to a JSON snapshot for cold-start recovery.

## Hosting

**Primary**: Oracle Cloud Always Free Ampere A1 VM running Ubuntu, with
Caddy as a TLS-terminating reverse proxy in front of the Go binary on
`127.0.0.1:8080`. See [docs/oracle-setup.md](docs/oracle-setup.md) for
the full onboarding playbook. Deploys go through
`.github/workflows/deploy-oracle.yml` (cross-compile ARM64 → scp →
systemctl restart).

**Fallback**: `fly.toml` is preserved in the repo and
`.github/workflows/deploy.yml` (now `deploy-fly`) can be triggered
manually if you ever want to spin up a Fly machine instead. Note: Fly's
"free tier" is actually a $5/month trial credit + a card requirement —
not the no-strings free tier this app was originally built on.

## Known limitations

- "Most played in top tournaments" isn't always "best to craft" for a
  budget player. A budget-mode toggle is on the roadmap.
- Rotation dates are *estimated* (`set_release + 3 years`) — close enough
  for the multiplier, but not authoritative.
- MTGTop8 doesn't carry MTGA-only formats; we cover paper formats only.
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
