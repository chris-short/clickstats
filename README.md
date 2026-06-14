# clickstats

Click analytics dashboard for [Buttondown](https://buttondown.com) newsletters. Shows which links your readers actually click, ranked by frequency, with per-issue breakdowns and a sponsor performance PDF export.

## What it does

- Fetches click events from the Buttondown API and aggregates them by URL
- Web dashboard with all-time top 50 links, per-issue breakdown, domain aggregation, and a 10-issue trend chart
- Domain drill-down: click any domain to see all individual links for it
- Bottom 100 links view: see which links got the least engagement
- Sponsor report: generate a print-ready PDF showing a sponsor link's rank, clicks, and click rate
- Two-tier cache: 10-minute in-memory TTL backed by a persistent disk cache (`~/.clickstats/cache.json`) so restarts are instant and old issue data is never re-fetched
- CLI mode for quick one-off queries

## Prerequisites

- Go 1.22+
- A Buttondown API key (`Settings > API keys`)

## Build

```bash
git clone https://github.com/chrisshort/clickstats
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

## Self-hosting on Fly.io

Fly.io runs a persistent container - no local infrastructure, no VMs to manage, free tier covers this comfortably.

1. Install the Fly CLI: https://fly.io/docs/hands-on/install-flyctl/

2. Authenticate:
   ```bash
   fly auth login
   ```

3. Launch (run once from the repo root):
   ```bash
   fly launch --name clickstats --region ord --no-deploy
   ```

4. Set your API key and newsletter name as secrets:
   ```bash
   fly secrets set BUTTONDOWN_API_KEY=your_key
   fly secrets set CLICKSTATS_NAME="Your Newsletter"
   ```

   Use `fly secrets` rather than the `[env]` section of `fly.toml` for sensitive values. Secrets are encrypted at rest, never appear in config files or deployment logs, and are injected as environment variables at runtime. To see what secrets are set (without revealing values): `fly secrets list`.

5. Deploy:
   ```bash
   fly deploy
   ```

The dashboard will be live at `https://clickstats.fly.dev` (or whatever name you chose). Subsequent deploys are just `fly deploy`.

To update the newsletter name without redeploying: `fly secrets set CLICKSTATS_NAME="New Name"` triggers an automatic redeploy.

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
