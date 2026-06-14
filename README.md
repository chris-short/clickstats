# clickstats

Click analytics dashboard for [Buttondown](https://buttondown.com) newsletters. Shows which links your readers actually click, ranked by frequency, with per-issue breakdowns and a sponsor performance PDF export.

## What it does

- Fetches click events from the Buttondown API and aggregates them by URL
- Web dashboard with all-time top links, per-issue breakdown, and a 10-issue trend chart
- Sponsor report: generate a print-ready PDF showing a sponsor link's rank, clicks, and click rate
- 10-minute in-memory cache so the dashboard stays fast
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

You can also set `CLICKSTATS_NAME` as an environment variable instead of using `--name`.

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

4. Set your API key as a secret:
   ```bash
   fly secrets set BUTTONDOWN_API_KEY=your_key
   fly secrets set CLICKSTATS_NAME="Your Newsletter"
   ```

5. Deploy:
   ```bash
   fly deploy
   ```

The dashboard will be live at `https://clickstats.fly.dev` (or whatever name you chose). Subsequent deploys are just `fly deploy`.

To update the newsletter name without redeploying: `fly secrets set CLICKSTATS_NAME="New Name"` triggers an automatic redeploy.

## License

MIT - see [LICENSE](LICENSE)
