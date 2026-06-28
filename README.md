<p align="center">
  <img src="logo/clickstats.png" alt="clickstats logo" width="200">
</p>

# clickstats

Click analytics dashboard for [Buttondown](https://buttondown.com) newsletters. Shows which links your readers actually click, ranked by frequency, with per-issue breakdowns and a sponsor performance PDF export.

**Pull requests welcome**

## What it does

- Fetches click events from the Buttondown API and aggregates them by URL
- Web dashboard with all-time top 50 links, per-issue breakdown, domain aggregation, and a 10-issue trend chart
- Domain drill-down: click any domain to see all individual links for it
- Bottom 100 links view: see which links got the least engagement
- Sponsor report: generate a print-ready PDF showing a sponsor link's rank, clicks, and click rate
- Two-tier cache: 10-minute in-memory TTL backed by a persistent disk cache (`~/.clickstats/cache.json`) so restarts are instant and old issue data is never re-fetched
- CLI mode for quick one-off queries

## Prerequisites

- A Buttondown API key (`Settings > API keys`)

## Install

### Homebrew (macOS and Linux)

```bash
brew tap chris-short/clickstats
brew install clickstats
```

### Build from source

Requires Go 1.22+.

```bash
git clone https://github.com/chris-short/clickstats
cd clickstats
go build -o clickstats .
```

## Usage

### Web dashboard

```bash
export BUTTONDOWN_API_KEY=your_key
./clickstats serve
# Open http://127.0.0.1:8080
```

Options:
- `--port 8080` - port to listen on (default: 8080)
- `--host 127.0.0.1` - host to bind (default: 127.0.0.1)
- `--name "My Newsletter"` - name shown in the dashboard (default: DevOps'ish)
- `--cache-dir ~/.clickstats` - directory for the persistent disk cache

Environment variable equivalents: `CLICKSTATS_NAME`, `CLICKSTATS_CACHE_DIR`.

### CLI

```bash
# All-time top links
BUTTONDOWN_API_KEY=your_key ./clickstats

# Filter to a specific issue number
BUTTONDOWN_API_KEY=your_key ./clickstats --issue 322

# Filter by email UUID directly
BUTTONDOWN_API_KEY=your_key ./clickstats --email-id abc-123-...

# Inspect raw click event metadata (useful if links aren't resolving)
BUTTONDOWN_API_KEY=your_key ./clickstats --debug
```

## Self-hosting on Render

Render runs a persistent Docker container with no credit card required on the free tier. The `render.yaml` in this repo configures everything automatically.

1. Push the repo to GitHub (or fork it).

2. Create a free account at https://render.com and connect your GitHub.

3. Click **New > Blueprint** and select this repo. Render reads `render.yaml` and creates the service.

4. When prompted, set the required environment variables:
   - `BUTTONDOWN_API_KEY` - your Buttondown API key (`Settings > API keys`)
   - `CLICKSTATS_NAME` - your newsletter name (e.g. `DevOps'ish`)

   Set these in the Render dashboard under **Environment**, not in `render.yaml` - the file intentionally leaves them blank so secrets never land in the repo.

5. Click **Deploy**. The dashboard will be live at `https://clickstats.onrender.com` (or your chosen name).

Subsequent deploys happen automatically on every push to the default branch.

**Free tier note:** Render spins down services after 15 minutes of inactivity. The next request after idle takes about 50-60 seconds to cold-start. Once running, the cache warms normally. If you check the dashboard regularly this is a non-issue; if you want it always-on, a free cron ping service (e.g. UptimeRobot on a 10-minute interval pointing at `/api/config`) keeps it awake.

**Disk cache on Render free tier:** Render's free tier does not provide persistent disks, so the on-disk cache (`~/.clickstats/cache.json`) is cleared on every restart or deploy. The app handles this gracefully - it just re-fetches from Buttondown on the next request. Paid Render plans support persistent disks if you want cache to survive restarts.

## License

MIT - see [LICENSE](LICENSE)
