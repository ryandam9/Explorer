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
| `a` | (Functions) **Activity** for a day — invocation count and a regex search of that day's logs ([below](#activity-invocations-and-log-search-for-a-day)) |
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

### Activity: invocations and log search for a day

Press **`a`** on a function to answer "what did it do on this day?" — e.g. for
an S3-triggered function that copies each new object elsewhere: how many times
it ran on the 24th, and which objects it copied.

A small form asks for a **date** (`YYYY-MM-DD`, `today` or `yesterday`, in your
local time zone), a **regex**, and an optional **server filter**. The report
then shows:

- **Invocations** — the day's `AWS/Lambda` `Invocations`, `Errors` and
  `Throttles` (Sum), from one batched `GetMetricData` call, with a per-hour
  sparkline and the peak hour. The count is every invocation, retries of a
  failed async (S3) event included — the errors figure next to it shows how
  many of those failed. A day with no datapoints says so instead of
  showing a bare `0`: the API can't tell "not invoked" from "older than
  CloudWatch's 455-day retention for hourly data".
- **Log scan** — the day's events from the function's log group (its
  `LoggingConfig` group, else `/aws/lambda/<name>`), read page by page with
  `FilterLogEvents` (so the table fills in live), keeping those the regex
  matches. Each match is a row: **time** (to the millisecond), **request ID**,
  **level**, one column per **capture group** (named groups by name, others as
  `$1`, `$2`…; the matched text when there are no groups), and the message
  with the runtime's timestamp/ID/level prefix removed. A long message wraps
  across the full width of the panel instead of being cut off, and a multi-line one
  (a Python traceback) keeps its line breaks. A match takes at most 8 lines;
  beyond that the last line says how many were left out and `y` copies the whole
  line. `↑/↓` move a whole match at a time. The footer shows the selected
  match with its log stream.
  The scan also counts **START lines**, the invocations the logs saw, as a
  cross-check on the metric.

The regex is Go (RE2) syntax, run against the whole raw log line — prefix
`(?i)` for case-insensitive. Request IDs are read from the Node.js/Python text
formats, the platform `START/END/REPORT` lines and the JSON log format; for a
bare `print()` line the ID is taken from the stream's last `START` (an
execution environment runs one invocation at a time) and marked with `~`.

| Key | Action |
|-----|--------|
| `↑/↓` `PgUp/PgDn` `g/G` | Move through the matches |
| `[` / `]` | Previous / next day, same query |
| `e` | Edit the query (date, regex, filter) |
| `r` | Run it again |
| `y` | Copy the selected match's full log line |
| `<` / `>` | Scroll the columns |
| `Esc` | Stop a running scan (keeping what was read); again to go back |

**Bounds, said out loud.** A busy function's day can hold millions of lines, so
the scan stops at 1,000 matches or 1,000 log pages. When either bound ends it
the report says so ("stopped at 1000 matches — more may exist later in the
day"), and the START count is marked partial. The **server filter** (CloudWatch
[filter-pattern syntax](https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/FilterAndPatternSyntax.html),
e.g. `"Copied"` or `%orders-\d+%`) is applied by CloudWatch before the regex, so
far less data is read. START lines are then filtered out too, and the count
is shown as not counted rather than as 0. A custom log group that other
functions also write to is flagged in the header.

Cost: 3 metrics per `GetMetricData` run (billed per metric requested, fractions
of a cent), and nothing auto-refreshes. The log search uses `FilterLogEvents`,
not a Logs Insights query (billed per GB scanned).

### Scriptable twins

Every pane has a non-interactive command for pipelines and `jq`:

```bash
aws_explorer lambda functions      [--all-regions] [-o table|json|ndjson|csv]
aws_explorer lambda layers         [-o …]
aws_explorer lambda event-sources  [-o …]   # aliases: event-source-mappings, esm
aws_explorer lambda activity <function|arn> [--date YYYY-MM-DD|today|yesterday] [--utc]
                             [-p REGEX] [--filter PATTERN] [--limit N] [--log-group G] [-o …]
```

`lambda activity` is the Activity report as a command. `table` prints the
summary, the hourly breakdown and the matches; `json` the whole report (hours
with no datapoint are `null`, not `0`); `ndjson` and `csv` one row per match —
per hour without `--pattern`. CSV cells are formula-neutralised, since log lines
are arbitrary text. A bare function name is looked up across the regions in
scope; a name found in several asks for `--region`.

```bash
# How many times did it run yesterday?
aws_explorer lambda activity copy-object --date yesterday

# Which objects did it copy on the 24th? Named groups become columns / JSON keys
aws_explorer lambda activity copy-object --date 2026-09-24 \
    -p 'Copied s3://(?P<src>\S+) to s3://(?P<dst>\S+)' -o json \
  | jq -r '.logScan.matches[] | [.time, .groups.src, .groups.dst] | @tsv'

# Every error line that day, read less data with a server-side filter first
aws_explorer lambda activity copy-object -p '(?i)error|exception' --filter '?ERROR ?Exception' -o csv
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
uses the `cw` command's existing `logs:*` read actions. Activity adds
`cloudwatch:GetMetricData` (invocation counts), `logs:FilterLogEvents` (the log
scan) and, for `lambda activity`, `lambda:GetFunctionConfiguration` (region and
log group lookup; if denied in a single-region scope it falls back to
`/aws/lambda/<name>` with a warning). A denied metrics call leaves the count
*unknown*, with the reason shown, and doesn't stop the log scan. The other way
round works the same. Any per-region or
per-listing denial degrades just that part of the dashboard with a logged note
and never aborts the session.
