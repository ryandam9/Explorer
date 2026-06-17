# AWS Lambda dashboard

`lambda` opens an interactive dashboard for AWS Lambda. Tab across **Functions**,
**Layers** and **Event sources** (event-source mappings); each row shows health
at a glance — a function's runtime, memory, timeout and state, a layer's latest
version and compatible runtimes, an event-source mapping's source, state and
batch size. Press **Enter** on a function to open its full configuration as a
**grid of panels** (btop-style, like `emr` describe) — Overview, Resources &
limits, State, **VPC networking**, **Environment** (variable keys only — values
are never shown), **Layers**, **Code package** and **Tags** — each a separately
scrollable tile, fetched on demand. `Tab`/arrows move focus between tiles.
**Enter** on a layer or event source opens its panels from the loaded data.

```bash
./bin/aws_explorer lambda [--region us-east-1 | --all-regions] [--theme <name>]
```

```
 Lambda ▸ Functions (4)  Layers (2)  Event sources (3)

 NAME                  RUNTIME       MEMORY   TIMEOUT  STATE       LAST MODIFIED
 orders-api            python3.12    256 MB   30s      ✓ Active    2026-06-15 01:14
 legacy-cron           python3.9     128 MB   60s      ✓ Active    2025-02-02 09:00
 image-thumbs          Image         1024 MB  900s     ✓ Active    2026-06-10 18:22
 broken-deploy         nodejs20.x    512 MB   15s      ✗ Failed    2026-06-16 22:01
```

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Switch pane (or, in the detail view, move focus between panels) |
| `↑/↓` (`j/k`) | Move selection (or scroll the focused detail panel) |
| `Enter` | Open the selected resource's **detail grid** — a full-screen set of per-section panels (functions fetch their configuration on demand; layers and event sources render from loaded data). `Tab`/arrows move between tiles; the focused tile scrolls |
| `f` | **Findings** — deterministic runtime/health checks (deprecated or soon-deprecating runtimes, missing dead-letter queues, failed-state functions) over the loaded functions; `y` copies the suggested fix |
| `L` | (Functions) open the function's CloudWatch logs (`/aws/lambda/<name>`) |
| `S` / `R` | Cycle the active tab's sort column / reverse the direction (resets on a tab switch) |
| `/` | Filter the current pane |
| `o` | Open the selected resource in the AWS console |
| `r` | Refresh |
| `~` | Debug pane — a live view of what the tool is doing (the scan's activity log) |
| `i` | About this page · `q` quit |

The **Findings** panel reuses the same deterministic checks as `audit`
(`LAM-RUN-*`, `LAM-CFG-*`) over the data already on screen — no extra AWS calls.
Every Lambda check is evaluable from the function list (runtime, dead-letter
config, state), so the panel shows the full set rather than a suppressed subset.

**Environment-variable values are never collected or rendered** — the detail
panel shows only the *keys* (and their count), so a secret passed as an env var
can't leak onto the screen or into a screenshot.

### Scriptable twins

Every pane has a non-interactive command for pipelines and `jq`:

```bash
aws_explorer lambda functions      [--all-regions] [-o table|json|ndjson|csv]
aws_explorer lambda layers         [-o …]
aws_explorer lambda event-sources  [-o …]   # aliases: event-source-mappings, esm
```

```bash
# Which functions are on a deprecated/old runtime?
aws_explorer lambda functions -o json | jq '[.[] | select(.runtime | startswith("python3.7")) | .name]'

# Functions with no dead-letter queue
aws_explorer lambda functions -o json | jq '[.[] | select(.hasDeadLetterQueue == false) | .name]'

# Event-source mappings that are disabled
aws_explorer lambda event-sources -o json | jq '.[] | select(.state != "Enabled") | {function, source, state}'
```

The functions JSON exposes machine-readable `memoryMB`, `timeoutSeconds`,
`runtime`, `packageType`, `state`, `hasDeadLetterQueue` and ISO-8601
`lastModified`.

### Findings (also in `audit --only lambda`)

| ID | Finding | Severity |
|----|---------|----------|
| `LAM-RUN-001` | Function on a **deprecated runtime** (updates blocked) | 🟡 |
| `LAM-RUN-002` | Function's runtime is **approaching deprecation** (within 90 days) | 🔵 |
| `LAM-CFG-001` | Function has **no dead-letter queue** (failed async invocations dropped unless an on-failure destination is set) | 🔵 |
| `LAM-CFG-002` | Function is in a **failed state** (or its last update failed) | 🟡 |

The runtime checks read the same end-of-life table as `expiring`
(`internal/expiry/eol.go`); a runtime missing from that table simply doesn't
fire (the linter under-warns rather than mis-warns). The DLQ check is worded
honestly — it reports what is known (no DLQ) without asserting events are
definitely being dropped, since an on-failure destination is a valid
alternative that the list API does not expose.

**IAM permissions.** Read-only:
`lambda:{ListFunctions,GetFunction,ListLayers,ListEventSourceMappings}` and
`sts:GetCallerIdentity` (for the console-link account fallback). The `L` jump
uses the `cw` command's existing `logs:*` read actions. Any per-region or
per-listing denial degrades just that part of the dashboard with a logged note
and never aborts the session.
