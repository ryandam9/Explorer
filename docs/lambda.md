# AWS Lambda dashboard

`lambda` opens an interactive dashboard for AWS Lambda. Tab across **Functions**,
**Layers** and **Event sources** (event-source mappings); each row shows health
at a glance — a function's runtime, memory, timeout and state, a layer's latest
version and compatible runtimes, an event-source mapping's source, state and
batch size. Once the list is on screen, each function's **invocations over the
last 30 days** and the **day it was last invoked** fill in (a background load —
see [Usage and access](#usage-and-access)). Press **Enter** on a function to
open its full configuration as a **grid of panels** (btop-style, like `emr`
describe) — Overview, Resources & limits, State, **VPC networking**,
**Environment** (variable keys only — values are never shown), **Layers**,
**Code package**, **Resource policy**, **Triggers**, **Versions & aliases**,
**Function URL**, **Async invocation** and **Tags** — each a separately
scrollable tile, fetched on demand. `Tab`/arrows move focus between tiles.
**Enter** on a layer or event source opens its panels from the loaded data.

```bash
./bin/aws_explorer lambda [--region us-east-1 | --all-regions] [--theme <name>]
```

```
 Lambda ▸ Functions (4)  Layers (2)  Event sources (3)

 NAME           RUNTIME     MEMORY   TIMEOUT  STATE      INVOKES 30D  LAST INVOKED  LAST MODIFIED
 orders-api     python3.12  256 MB   30s      ✓ Active   48,210       2026-06-17    2026-06-15 01:14
 legacy-cron    python3.9   128 MB   60s      ✓ Active   0            none in 30d   2025-02-02 09:00
 image-thumbs   Image       1024 MB  900s     ✓ Active   1,320        2026-06-16    2026-06-10 18:22
 broken-deploy  nodejs20.x  512 MB   15s      ✗ Failed   ?            ?             2026-06-16 22:01
```

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Switch pane (or, in the detail view, move focus between panels) |
| `↑/↓` (`j/k`) | Move selection (or scroll the focused detail panel) |
| `Enter` | Open the selected resource's **detail grid** — a full-screen set of per-section panels (functions fetch their configuration on demand; layers and event sources render from loaded data). `Tab`/arrows move between tiles; the focused tile scrolls |
| `f` | **Findings** — deterministic checks (deprecated runtimes, missing dead-letter queues, failed state, idle functions, log groups that never expire, public function URLs/policies, arm64 candidates) over the loaded functions; `y` copies the suggested fix |
| `a` | (Functions) **Activity** for a day or a range — invocation counts, performance and a regex search of the logs ([below](#activity-invocations-and-log-search-for-a-day)) |
| `L` | (Functions) open the function's CloudWatch logs (`/aws/lambda/<name>`) |
| `S` / `R` | Cycle the active tab's sort column / reverse the direction (resets on a tab switch; numbers sort by value) |
| `/` | Filter the current pane |
| `o` | Open the selected resource in the AWS console |
| `r` | Refresh |
| `~` | Debug pane — a live view of what the tool is doing (the scan's activity log) |
| `i` | About this page · `q` quit |

The **Findings** panel runs the deterministic checks ([below](#findings)) over
the data already on screen — the function list plus the background usage load —
with no extra AWS calls of its own. While that load is still running the panel
says so; a read it couldn't make is listed at the top, and the checks that need
it stay silent rather than guessing.

**Detail panels for how a function is invoked:**

- **Triggers** — what calls it, from what AWS records: the event-source
  mappings that poll a queue or stream for it, the principals its resource
  policy lets invoke it (an S3 bucket, EventBridge rule, SNS topic, API Gateway,
  another account…, with the source each grant is scoped to), and its function
  URLs. Callers using their own IAM permissions in the same account (Step
  Functions, SDK calls, other functions) need no policy entry, so the panel says
  it can't list those rather than implying "nothing calls this".
- **Versions & aliases** — aliases with their weighted routing
  (`live → v12 (90%) + v11 (10%)`) and provisioned concurrency, then the
  published versions, newest first.
- **Function URL** — the URL, its auth type (`NONE` is flagged: anyone on the
  internet can call it), the alias it points at, invoke mode and CORS origins.
- **Async invocation** — retries, maximum event age, the on-success/on-failure
  destinations and the dead-letter queue: where a failed async event ends up.

A denied read shows as *access denied* in its panel, never as "none".

**Environment-variable values are never collected or rendered** — the detail
panel shows only the *keys* (and their count), so a secret passed as an env var
can't leak onto the screen or into a screenshot.

### Activity: invocations and log search for a day

Press **`a`** on a function to answer "what did it do on this day?" — e.g. for
an S3-triggered function that copies each new object elsewhere: how many times
it ran on the 24th, and which objects it copied.

A small form asks for a **date or range** (`YYYY-MM-DD`, `today`, `yesterday`, a
range `2026-09-20..2026-09-24` of up to 31 days, or `7d` for the last seven days
including today — all in your local time zone), a **regex**, and an optional
**server filter**. A range reads and reports the same way as a day, with a
per-day sparkline and the peak day, and times in the table carry the date. Leave the
regex empty to list **every event** of the day (with a server filter: every
event it keeps) — the table then has no MATCH column and is bounded the same
way, at 1,000 events. The report then shows:

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
- **Performance** — from the platform's `REPORT` line (one per invocation):
  p50/p95/max **duration** against the timeout, **max memory used** against the
  memory size, **cold starts** with their init time, and an **estimated compute
  cost** (billed GB-seconds × the on-demand x86/arm64 price, plus requests —
  us-east-1 list prices, before the free tier). No extra AWS call: the scan
  already reads these lines. A server filter usually hides them, so the figures
  then read "not measured", never 0; a scan stopped at a bound says "partial".
- **Failed invocations** — a run is marked failed (`✗` on its rows) when one of
  its lines shows it unambiguously: a line logged at `ERROR`/`FATAL`/`CRITICAL`
  (including Python's unhandled-exception line), `Task timed out`, a runtime
  exit, or a `REPORT` status of `timeout`/`error`. The word "error" inside an
  INFO message does not count. **`E`** jumps to the next match whose run failed.
- **The whole invocation** — **`Enter`** on a match opens every line of the run
  it belongs to, `START` to `REPORT`, read back from its log stream (one
  `FilterLogEvents` call scoped to that stream and ±timeout around the line),
  including a cold start's init output. The header shows the run's duration,
  memory and status; `y` copies a line, `Y` the whole run, `Esc` goes back.

The regex is Go (RE2) syntax, run against the whole raw log line — prefix
`(?i)` for case-insensitive. Request IDs are read from the Node.js/Python text
formats, the platform `START/END/REPORT` lines and the JSON log format; for a
bare `print()` line the ID is taken from the stream's last `START` (an
execution environment runs one invocation at a time) and marked with `~`.

| Key | Action |
|-----|--------|
| `↑/↓` `PgUp/PgDn` `g/G` | Move through the matches |
| `Enter` | Open the match's whole invocation (every line of that run) |
| `E` | Jump to the next match whose invocation failed |
| `[` / `]` | Previous / next day (or range, by its own length), same query |
| `e` | Edit the query (date, regex, filter) |
| `r` | Run it again |
| `y` | Copy the selected match's full log line |
| `X` | Export the rows to an Excel workbook ([below](#excel-export)) |
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

#### Excel export

`X` writes the report's rows to an `.xlsx` workbook in the downloads directory
(`app.downloadDir`, default `~/.aws_explorer/downloads`), named after what it
holds, e.g. `lambda-stocks-notify-2026-09-24-matches-20260925-143012.xlsx`
(`-events-` when the regex was empty). The status bar shows the path.

- **`Matches <day>`** (or **`Events <day>`**) — one row per match: the time as a
  real date-time in the day's zone (named in the header), the full request ID
  (plus a "Request ID from START" column when any ID was inferred), level, one
  column per capture group, the full message (wrapped, line breaks kept, never
  cut with `…`) and the log stream. The header row is frozen and filterable.
  An "Invocation failed" column appears when any run failed.
- **`Query`** — what produced the rows: function, region, log group, day, regex,
  server filter, the invocation counts, the scan summary, the performance
  figures, the failed-invocation count, and whether the scan was complete or
  stopped at a bound (or by Esc) — so a partial export says so.

The sheets are plain: no gridlines, a white background, a blue header band,
and thin borders only around the cells that hold data. Log text is always
written as text, so a line that starts with `=` can't run as a formula. A
running scan isn't exported — wait for it, or Esc to stop it and export what
was read.

Cost: 3 metrics per `GetMetricData` run (billed per metric requested, fractions
of a cent), and nothing auto-refreshes. The log search uses `FilterLogEvents`,
not a Logs Insights query (billed per GB scanned).

### Scriptable twins

Every pane has a non-interactive command for pipelines and `jq`:

```bash
aws_explorer lambda functions      [--all-regions] [-o table|json|ndjson|csv]
aws_explorer lambda layers         [-o …]
aws_explorer lambda event-sources  [-o …]   # aliases: event-source-mappings, esm
aws_explorer lambda activity <function|arn> [--date YYYY-MM-DD|today|yesterday|A..B|Nd] [--utc]
                             [-p REGEX | --all-events] [--filter PATTERN] [--limit N] [--log-group G]
                             [-o table|json|ndjson|csv|xlsx] [--out FILE]
```

`lambda activity` is the Activity report as a command. `table` prints the
summary (with the performance and failed-invocation lines), the hourly — or,
for a range, daily — breakdown and the matches; `json` the whole report (hours
with no datapoint are `null`, not `0`; `logScan.performance` holds the REPORT
figures, `null` when not measured; each match carries `invocationFailure`);
`ndjson` and `csv` one row per match — per hour without a scan. `-o xlsx`
(this command only) writes the same workbook as `X` in the TUI to the
downloads directory, or to `--out FILE`, and prints its path. `--all-events`
lists every event of the window instead of searching (the TUI's empty regex). CSV cells are formula-neutralised, since log lines
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

# A week's p95 duration and estimated cost
aws_explorer lambda activity copy-object --date 7d --all-events -o json \
  | jq '.logScan.performance | {runs, durationP95Ms, estimatedCostUsd}'

# Every event of the last week as an Excel workbook
aws_explorer lambda activity copy-object --date 7d --all-events -o xlsx
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

### Usage and access

After each load (and `r`), the dashboard reads in the background, per region:

- **Invocations, last 30 days** — one `GetMetricData` call per 500 functions
  (daily `Invocations` Sum, all versions and aliases): the `INVOKES 30D` and
  `LAST INVOKED` columns. A function with no datapoint wasn't invoked (a
  measured `0`, "none in 30d"); a read that failed shows `?`, never `0`.
- **Log groups** — one paginated `DescribeLogGroups` over `/aws/lambda/`, plus
  a lookup for each function that logs to a custom group: retention and stored
  bytes.
- **Who can invoke it** — each function's resource policy (`GetPolicy`) and
  function URLs (`ListFunctionUrlConfigs`), in a pool of 10.

Failures are collapsed into one note per read ("couldn't read resource policies
(lambda:GetPolicy) — access denied for 20 function(s)"), shown in the Findings
panel. Cost: `GetMetricData` is billed per metric requested — about $0.00001
per function per load — and nothing auto-refreshes; the other reads are free
control-plane calls.

### Findings

| ID | Finding | Severity |
|----|---------|----------|
| `LAM-SEC-002` | **Resource policy lets anyone invoke** the function — `Principal: "*"` with no condition (Security Hub Lambda.1) | 🔴 |
| `LAM-RUN-001` | Function on a **deprecated runtime** (updates blocked) | 🟡 |
| `LAM-CFG-002` | Function is in a **failed state** (or its last update failed) | 🟡 |
| `LAM-SEC-001` | **Function URL with auth `NONE`** — anyone on the internet can call it | 🟡 |
| `LAM-RUN-002` | Function's runtime is **approaching deprecation** (within 90 days) | 🔵 |
| `LAM-CFG-001` | Function has **no dead-letter queue** (failed async invocations dropped unless an on-failure destination is set) | 🔵 |
| `LAM-USE-001` | Function **not invoked in 30 days** | 🔵 |
| `LAM-LOG-001` | Function's **log group never expires** (with what it stores and ≈ monthly cost) | 🔵 |
| `LAM-COST-001` | x86 function in use that **could run on arm64** (~20% cheaper per GB-second) | 🔵 |

`LAM-RUN-*` and `LAM-CFG-*` also run in `audit --only lambda`. The usage and
access checks (`LAM-USE`, `LAM-LOG`, `LAM-SEC`, `LAM-COST`) need the background
reads above, which `audit` doesn't make, so they appear only in the dashboard;
in `audit` their inputs are unknown and they stay silent.

- `LAM-USE-001` is informational: a monthly or on-demand job is legitimately
  quiet, so it asks you to confirm before deleting.
- `LAM-SEC-002` doesn't fire for a grant scoped by `AWS:SourceArn`,
  `AWS:SourceAccount` or `aws:PrincipalOrgID`, nor for the statement a
  `NONE`-auth URL needs (that is `LAM-SEC-001`).
- `LAM-COST-001` stays silent where moving isn't just a setting: container
  images, custom runtimes (compiled binaries), functions with layers (which may
  ship native x86 code), and functions that aren't invoked.

The runtime checks read the same end-of-life table as `expiring`
(`internal/expiry/eol.go`); a runtime missing from that table simply doesn't
fire (the linter under-warns rather than mis-warns). The DLQ check is worded
honestly — it reports what is known (no DLQ) without asserting events are
definitely being dropped, since an on-failure destination is a valid
alternative that the list API does not expose.

**IAM permissions.** Read-only:
`lambda:{ListFunctions,GetFunction,ListLayers,ListEventSourceMappings}` and
`sts:GetCallerIdentity` (for the console-link account fallback). The `L` jump
uses the `cw` command's existing `logs:*` read actions. The usage load adds
`cloudwatch:GetMetricData`, `logs:DescribeLogGroups`, `lambda:GetPolicy` and
`lambda:ListFunctionUrlConfigs`; the detail view adds
`lambda:{ListVersionsByFunction,ListAliases,ListFunctionUrlConfigs,ListProvisionedConcurrencyConfigs,GetFunctionEventInvokeConfig}`.
Activity adds `cloudwatch:GetMetricData` (invocation counts),
`logs:FilterLogEvents` (the log scan and the invocation drill-down) and, for
`lambda activity`, `lambda:GetFunctionConfiguration` (region and
log group lookup; if denied in a single-region scope it falls back to
`/aws/lambda/<name>` with a warning). A denied metrics call leaves the count
*unknown*, with the reason shown, and doesn't stop the log scan. The other way
round works the same. Any per-region or
per-listing denial degrades just that part of the dashboard with a logged note
and never aborts the session.
