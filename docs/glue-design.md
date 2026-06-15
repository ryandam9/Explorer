# AWS Glue Support — Design Specification

Status: **Proposed** · Theme J of the [enhancement roadmap](enhancement-roadmap.md)
(`AXE-001 … AXE-024`) · Tracking issue: [#194](https://github.com/ryandam9/aws_explorer/issues/194)

This document specifies a first-class AWS Glue feature set for `aws_explorer`.
It continues the roadmap's stable-ID scheme (`AXE-NNN`) under a new theme so the
work can be referenced unambiguously in commits, PRs and the tracking issue
(e.g. `AXE-027: Glue job-run history view`).

It follows the tool's established design principles:

1. **Deterministic, no AI** — every analysis is a pure function over data the
   Glue API returns, unit-testable with fixture snapshots (the pattern set by
   `internal/findings/messaging.go` and `internal/vpctui/findings.go`).
2. **Read-only by default** — the entire feature set only *describes* Glue;
   nothing starts, stops, or mutates a job, crawler or trigger. Any future
   "run this job" action would follow the Reachability-Analyzer
   cost-stating confirmation pattern (`internal/vpctui/analyzer.go`).
3. **Best-effort collection** — a denied Glue API call degrades a feature, never
   crashes it; partial results are kept and flagged (the `Collector` contract in
   `internal/services/service.go`).
4. **One UX language** — every new table/overlay/detail panel uses the shared
   theme/table/key-hint machinery in `internal/ui` and `internal/table`; findings
   render in the existing severity/resource/issue/fix style.

---

## Why Glue, and what's there today

AWS Glue is AWS's serverless data-integration service. Its moving parts —
**ETL jobs** (managed Apache Spark or Python-shell scripts), **job runs**
(individual executions), **crawlers** (schema discovery into the Data Catalog),
**triggers** (schedule / on-demand / conditional / event starts),
**workflows** (DAGs of jobs + crawlers + triggers), **connections**
(JDBC / network), and the **Data Catalog** (databases, tables, partitions) — are
exactly the kind of thing engineers debug from the console all day:

> "Did last night's job succeed?" · "Why did this run fail?" · "Where are the
> logs?" · "How many DPU-hours is this job burning?" · "Which crawler hasn't
> finished?" · "What worker type / role / script is this job actually
> configured with?"

**Current state.** `internal/services/glue/collector.go` already lists three
resource types into the account-wide inventory — `database`, `job`, `crawler` —
using `GetDatabases` / `GetJobs` / `GetCrawlers`. They appear in `summary`, the
fuzzy finder, `whereused`, snapshot diffs and the console-link `o` fallback.
What's missing is **everything operational**: run history, run state, logs, the
job definition itself, triggers, workflows, connections, cost, and any Glue-aware
findings. This document closes that gap.

### Mapping Glue to the tool's surfaces

| Glue concept | API (read-only) | Where it lands |
|--------------|-----------------|----------------|
| Job definition | `GetJobs` / `GetJob` | inventory + detail panel (AXE-025, AXE-029) |
| Job run history | `GetJobRuns` / `GetJobRun` | runs view + dashboard (AXE-027) |
| Job run logs | CloudWatch `/aws-glue/jobs/{output,error,logs-v2}` keyed by `JobRunId` | log jump (AXE-028) |
| Crawler + last crawl | `GetCrawlers` / `GetCrawler` / `GetCrawlerMetrics` | inventory + dashboard (AXE-025, AXE-026) |
| Trigger | `GetTriggers` | dashboard tab (AXE-026) |
| Workflow | `ListWorkflows` / `GetWorkflow` | dashboard tab (AXE-026) |
| Connection | `GetConnections` | dashboard tab (AXE-026) |
| Database / table | `GetDatabases` / `GetTables` | catalog tab (AXE-026) |
| Health / waste | derived from the above | `audit` category (AXE-031) |
| Deep links | pure string mapping | `o` in every Glue surface (AXE-032) |

---

## Contents

| ID | Title | Priority |
|----|-------|----------|
| [AXE-025](#axe-025) | Glue collector enrichment (run state, definition, last crawl) | **P1** · ✅ shipped |
| [AXE-026](#axe-026) | `glue` interactive dashboard TUI | **P1** · ✅ shipped |
| [AXE-027](#axe-027) | Job-run history view (state · duration · DPU-hours · error) | **P1** · ✅ shipped |
| [AXE-028](#axe-028) | Jump from a job run to its CloudWatch logs | **P2** · ✅ shipped |
| [AXE-029](#axe-029) | Job definition / settings detail panel | **P2** · ✅ shipped |
| [AXE-030](#axe-030) | CLI twins (`glue jobs` / `runs` / `crawlers` / …) | **P2** · ✅ shipped |
| [AXE-031](#axe-031) | `glue` audit category (health & cost findings) | **P2** · ✅ shipped |
| [AXE-032](#axe-032) | Glue console deep links | **P3** · ✅ shipped |

Priorities: **P1** = build first, **P2** = next, **P3** = deferrable. A suggested
phasing is at the [end of this document](#phasing).

---

## Architecture context

Where the new code plugs in (all paths confirmed against the current tree):

- **Collector** — `internal/services/glue/collector.go` already exists and
  implements the `Collector` interface (`internal/services/service.go`) and is
  registered in `internal/services/registry.go`. AXE-025 extends it with a
  `DetailLevelDetailed` pass and new resource types; it keeps streaming
  page-sized batches via `CollectInput.EmitOrAppend`.
- **Engine** (`internal/engine`) fans the collector across regions with a
  bounded pool, keeps partial results, and aggregates errors — no change needed.
- **TUI** — the dashboard (AXE-026/027) is a new Bubble Tea package
  `internal/gluetui`, modelled on `internal/cwtui` (model/update/view,
  `NewModel` factory, focus enum, `textinput` filters, spinner, toast) and built
  from `internal/table` + `internal/ui` (themes, key hints, overlays, window
  titles). The command (`cmd/glue.go`) mirrors `cmd/cw.go`.
- **Log jump** (AXE-028) reuses the `internal/cwtui` constructor that accepts an
  initial group + stream filter, exactly as the summary TUI's `L` key does for
  Lambda/RDS/EKS (`internal/loggroup`, AXE-011 precedent).
- **Findings** (AXE-031) add `internal/findings/glue.go` — a `GlueSnapshot`
  struct + pure `AnalyzeGlue(snap) []Finding`, check-ID constants, and
  registration in `internal/findings/checks.go`; the category is wired into
  `cmd/audit.go`'s `auditCategories` and `internal/audit/`.
- **Console links** (AXE-032) add a `case "glue"` to `internal/consolelink`'s
  `deepLink()` switch.
- **Output** (`internal/output`, `internal/csvexport`) renders the CLI twins
  (AXE-030) in table / JSON / NDJSON / CSV with zero new format code.
- **Docs** — a new guide `internal/docsgen/guides/tui-glue.md` registered in
  `docsgen.go`'s `guideList`; per-command reference pages auto-generate from the
  cobra tree.

### New / changed shared pieces

| Piece | Introduced by | Purpose |
|-------|---------------|---------|
| `internal/gluetui` | AXE-026/027/028/029 | Bubble Tea dashboard, modelled on `cwtui` |
| `internal/findings/glue.go` | AXE-031 | `GlueSnapshot` + `AnalyzeGlue` pure checks |
| `internal/costs` (extend) | AXE-027/031 | DPU-hour price constant + run-cost helper |
| `consolelink` `case "glue"` | AXE-032 | job/crawler/db/trigger/workflow URLs |
| `loggroup` Glue derivation | AXE-028 | `JobRunId` → `/aws-glue/jobs/*` group + stream |

---

## Theme J — AWS Glue / data integration

### AXE-025 — Glue collector enrichment {#axe-025}

> **Status: ✅ shipped** (#196) — the collector lists `job` / `crawler` /
> `database` / `trigger` / `workflow` / `connection`, stamps each job's latest
> run state and each crawler's last-crawl status, and captures the full job
> definition (secrets redacted) at detailed scope. Pure, fixture-tested mapping.

**Problem.** The inventory knows a job/crawler/database *exists* but nothing
about its health. The single most useful fact — "did it succeed, and when?" — is
absent. Triggers, workflows and connections aren't collected at all.

**Scope.**
- **New resource types** (added to the existing collector): `trigger`
  (`GetTriggers`), `workflow` (`ListWorkflows` + `GetWorkflow` for the run
  summary), `connection` (`GetConnections`). Optionally `table` per database
  (`GetTables`) at `DetailLevelDetailed` only — tables can be numerous, so guard
  behind detailed scope.
- **Job enrichment** at `DetailLevelSummary`+: for each job, one `GetJobRuns`
  call (page size 1, newest first) to stamp the latest run's state, start time
  and duration into the resource. Set `State` to the latest `JobRunState`
  (`SUCCEEDED` / `FAILED` / `RUNNING` / `TIMEOUT` / …) and populate `Summary`:

  ```
  Summary["lastRunState"]   = "FAILED"
  Summary["lastRunStarted"] = "2026-06-15T01:14:00Z"
  Summary["lastRunSeconds"] = "742"
  Summary["glueVersion"]    = "4.0"
  Summary["workerType"]     = "G.1X"
  ```
- **Crawler enrichment**: `GetCrawler` already returns `LastCrawl`
  (`Status`, `ErrorMessage`, `StartTime`); stamp `Summary["lastCrawlStatus"]`
  and keep `State` (READY / RUNNING / STOPPING).
- A `DetailLevelDetailed` pass captures the full job definition blob into
  `Details` (see AXE-029) and crawler targets/schedule.

**Implementation.**
- Extend `internal/services/glue/collector.go`. Each new listing is independent
  and best-effort: a failure in `collectTriggers` is joined into the returned
  error but never blocks `collectJobs` (the existing `errors.Join` pattern).
- Job-run enrichment is one extra paginated-to-1 call per job; cap concurrency
  and skip with a note if `glue:GetJobRuns` is denied (jobs still listed,
  `State` empty).
- ARNs continue to be synthesized to match the Tagging API form (`arn(...)`),
  extended for `trigger/`, `connection/`, and the workflow form.

**IAM needed.** `glue:GetJobs`, `glue:GetJobRuns`, `glue:GetCrawlers`,
`glue:GetCrawler`, `glue:GetTriggers`, `glue:ListWorkflows`,
`glue:GetWorkflow`, `glue:GetConnections`, `glue:GetDatabases`,
`glue:GetTables` (detailed only).

**Acceptance criteria.**
- Jobs in `summary` show a last-run state at a glance; crawlers show last-crawl
  status.
- A denied `GetJobRuns` lists jobs without state and records one note, never a
  crash.
- New types (`trigger`, `workflow`, `connection`) appear in `summary`, `find`,
  and JSON output with stable `Type` strings.

---

### AXE-026 — `glue` interactive dashboard TUI {#axe-026}

> **Status: ✅ shipped** — `aws_explorer glue` (`cmd/glue.go`, `internal/gluetui`).
> Tabbed Bubble Tea dashboard over Jobs / Crawlers / Triggers / Workflows /
> Connections / Catalog, colour-coded by state, with `/` filter, `o` console,
> `r` refresh and `i` about. Per-region/per-listing failures degrade softly.

**Problem.** Glue's console is a maze of separate pages (Studio, classic
console, Data Catalog). One terminal dashboard that shows jobs, their runs,
crawlers, triggers, workflows and connections side-by-side — with health at a
glance — is the headline feature this whole document builds toward.

**UX.**

```bash
aws_explorer glue                       # dashboard, all configured regions
aws_explorer glue --region us-east-1    # pin one region
aws_explorer glue --all-regions         # sweep + Region column
```

A tabbed Bubble Tea TUI (tab bar styled like the existing TUIs). Tabs:
`Jobs` · `Runs` · `Crawlers` · `Triggers` · `Workflows` · `Connections` ·
`Catalog`. Opens on **Jobs**.

```
 Glue ▸ Jobs   Runs   Crawlers   Triggers   Workflows   Connections   Catalog        us-east-1

  NAME                     LAST RUN     STATE       DURATION   WORKER     VERSION
▸ nightly-orders-etl       2026-06-15   ✓ SUCCEEDED   12m 22s   G.1X ×10   4.0
  customer-dedupe          2026-06-15   ✗ FAILED       2m 41s   G.2X ×5    4.0
  clickstream-flatten      2026-06-14   ● RUNNING      —        G.1X ×20   4.0
  legacy-export            (never run)  —              —        Standard   2.0

  enter runs · d definition · L logs · o console · / filter · tab next pane · ? help
```

State is colour-coded through the theme's role colours (success / error /
running / muted), the same vocabulary the audit and bill TUIs use.

**Keys** (registered via `internal/ui/keys.go` so the status bar stays truthful):
`tab` / `shift+tab` switch panes · `enter` drill into the selected row's runs
(AXE-027) · `d` open the job-definition detail panel (AXE-029) · `L` jump to the
selected run's logs (AXE-028) · `o` open in the console (AXE-032) · `/` filter ·
`r` refresh · `?` help · `q`/`esc` back/quit.

**Implementation.**
- New `internal/gluetui` package and `cmd/glue.go`, mirroring `internal/cwtui` /
  `cmd/cw.go`: `NewModel(ctx, awsCfg, regions, allRegions, appCfg, …) (tea.Model,
  error)`, a `focus` enum per pane, `internal/table.Model` per tab,
  `textinput.Model` for the filter, spinner + toast for async loads.
- Data comes from the AXE-025 collector at `DetailLevelSummary`, loaded
  asynchronously per region; rows stream in as they arrive (the engine already
  supports this), with a `cached N · refreshing…` style status while loading.
- `SilenceScanLogs()` so the TUI owns the screen; `tea.WithAltScreen()` and
  `ui.WithWindowTitle` as the other TUIs do.

**Acceptance criteria.**
- Launch with zero args renders the Jobs pane from the configured regions;
  per-region load failures show in an errors overlay, not a crash.
- Every pane is filterable; `o` yields a valid console URL on every row.
- Tab switching preserves each pane's selection.

---

### AXE-027 — Job-run history view {#axe-027}

> **Status: ✅ shipped** — `Enter` on a job in the dashboard opens its run
> history (newest first): state, duration, DPU-hours, an estimated cost
> (`internal/costs.GlueRunCostUSD`, $0.44/DPU-hour), worker and attempt, with
> the error message inline on failures and a DPU-hours/cost footer. Pure
> formatting/cost helpers are fixture-tested (`format_test.go`).

**Problem.** "Why did it fail, how long did it take, how much did it cost?" The
answer is in `GetJobRuns` but buried behind console clicks.

**UX.** `enter` on a job (dashboard) or `aws_explorer glue runs <job>` opens the
run history, newest first:

```
 Glue ▸ Runs — nightly-orders-etl                                       us-east-1

  STARTED            STATE        DURATION   DPU-HRS   EST    WORKER     ATTEMPT
▸ 2026-06-15 01:14   ✓ SUCCEEDED   12m 22s    2.06    $0.91   G.1X ×10   1
  2026-06-14 01:14   ✗ FAILED       2m 41s    0.45    $0.20   G.1X ×10   1
                     └ ErrorMessage: AnalysisException: Table or view not found: orders_raw
  2026-06-13 01:14   ✓ SUCCEEDED   11m 58s    1.99    $0.88   G.1X ×10   1

  L logs · y copy error · o console · esc back                  3 runs · 4.50 DPU-hrs ≈ $1.99
```

Columns from the `JobRun` object: `StartedOn`, `JobRunState`, `ExecutionTime`
(→ duration), `DPUSeconds` (→ DPU-hours = `DPUSeconds/3600`), an **estimated
cost** (`DPU-hours × $0.44`, clearly labelled "estimate"), `WorkerType` ×
`NumberOfWorkers` (or `MaxCapacity` for older jobs), and `Attempt`. A failed
row expands its `ErrorMessage`. The footer totals DPU-hours and cost over the
window.

**Implementation.**
- `internal/gluetui` runs view backed by `GetJobRuns` (paginated, capped to a
  configurable window — default last 20 runs / 7 days, `--since`/`--limit` on the
  CLI).
- DPU-hour → dollar conversion uses one constant in `internal/costs`
  (`GluePerDPUHourUSD = 0.44`, us-east-1 ETL, sourced comment), reusing the
  existing "this is an estimate" labelling convention.
- The run → row mapping is a pure function over `[]types.JobRun` → fixture-tested
  (success / failed-with-error / running / timeout / legacy-MaxCapacity cases).

**IAM needed.** `glue:GetJobRuns`.

**Acceptance criteria.**
- Runs sorted newest-first; failed runs show their error inline; running runs
  show no duration/cost.
- DPU-hours and estimate are correct for both worker-based and `MaxCapacity`
  jobs; a job with no runs shows "no runs in the last Nd", not an empty table.

---

### AXE-028 — Jump from a job run to its CloudWatch logs {#axe-028}

> **Status: ✅ shipped** — `L` in the run-history view suspends the dashboard
> and runs `aws_explorer cw --group <run log group> --stream <JobRunId>` as a
> child (the AXE-011 `tea.ExecProcess` pattern), inheriting `--region`/
> `--profile`/`--config`. The run's `LogGroupName` is used when present, else
> the `/aws-glue/jobs` base prefix (matching output/error/logs-v2); the run-ID
> stream filter matches both legacy and continuous-logging streams. Arg
> construction is pure and table-tested (`jump_test.go`).

**Problem.** The next click after "it failed" is always "show me the logs".
Glue writes them to predictable CloudWatch groups keyed by the run ID.

**UX.** `L` on a job (latest run) or on a specific run → opens the `cw` Logs TUI
pre-filtered to the run's log group and stream; `esc`/`q` returns to where you
were (the AXE-011 round-trip guarantee).

Glue log groups:

| Group | Contents |
|-------|----------|
| `/aws-glue/jobs/output` | driver/executor stdout (legacy logging) |
| `/aws-glue/jobs/error` | stderr / stack traces (legacy logging) |
| `/aws-glue/jobs/logs-v2` | continuous logging (when enabled) |

The log **stream** is the `JobRunId` (continuous logging uses
`<JobRunId>-driver` / `<JobRunId>` prefixes). `GetJobRun` also returns
`LogGroupName` for the run's configured group — prefer it when present.

**Implementation.**
- Add a Glue derivation to `internal/loggroup`:
  `glueRunLog(run) (group, streamPrefix string, ok bool)` — pure, table-tested.
- Reuse the `internal/cwtui` constructor that already accepts an initial group +
  stream filter (same mechanism the summary TUI's `L` uses). Off a TTY, degrade
  to a toast showing the group/stream name.
- When the group doesn't exist (logging disabled / not yet written), open the cw
  TUI with the name as a filter so the user sees "no streams match" in context.

**IAM needed.** the cw TUI's existing `logs:*` read actions.

**Acceptance criteria.**
- `L` lands on the run's stream when continuous logging is on; falls back to the
  error group otherwise; round-trip preserves the runs-view selection.

---

### AXE-029 — Job definition / settings detail panel {#axe-029}

> **Status: ✅ shipped** — `d` on a job in the dashboard opens an overlay with
> role, Glue version, execution class, worker, timeout, retries, script S3
> location, connections, security config, job-bookmark option and default
> arguments (secret-looking values redacted), fetched on demand via one
> `GetJob` call. Redaction/render helpers are fixture-tested.

**Problem.** "What is this job actually configured with?" — role, script, worker
type, version, timeout, retries, bookmarks, connections, default arguments. Today
that's a console-only read.

**UX.** `d` on a job opens a detail overlay (the existing detail-panel style):

```
 Job — nightly-orders-etl

  Role               arn:aws:iam::123…:role/glue-etl
  Glue version       4.0          Execution class  STANDARD
  Worker             G.1X × 10    Max capacity     —
  Timeout            2880 min     Max retries      1
  Job bookmark       job-bookmark-enable
  Script             s3://etl-scripts/nightly_orders.py
  Connections        prod-redshift, vpc-egress
  Security config    glue-sec-cmk
  Default arguments  --job-language=python · --enable-metrics=true · --TempDir=s3://…
                     (values for keys matching *secret*/*password*/*token* redacted)
```

Fields from the `Job` object: `Role`, `GlueVersion`, `ExecutionClass`,
`WorkerType`/`NumberOfWorkers`/`MaxCapacity`, `Timeout`, `MaxRetries`,
`Command.ScriptLocation`, `Connections.Connections`, `SecurityConfiguration`,
`DefaultArguments` (with secret-looking values redacted), and whether
`--job-bookmark-option` is enabled.

**Implementation.**
- The detailed collector pass (AXE-025) already stashes the `Job` blob in
  `Details`; the panel is a pure render over it. Redaction of secret-looking
  argument keys is a pure, tested function.
- The same data feeds AXE-031's configuration findings.

**Acceptance criteria.**
- Every defined field renders; absent fields show "—"; secret-looking default
  arguments are redacted in both the panel and JSON output.

---

### AXE-030 — CLI twins {#axe-030}

> **Status: ✅ shipped** — `glue jobs|crawlers|triggers|workflows` and
> `glue runs <job> [--limit] [--status]`, each honouring `-o
> table|json|ndjson|csv` and `--region`/`--all-regions`. Runs JSON exposes
> `durationSeconds`/`dpuHours`/`estUsd` and ISO timestamps. `glue` with no
> subcommand still launches the dashboard. Render + status-filter helpers are
> fixture-tested (`render_test.go`).

**Problem.** Everything the dashboard shows should be scriptable and pipeable —
the tool's standing contract (every TUI has a CLI twin).

**UX.**

```bash
aws_explorer glue jobs        [--all-regions] [-o table|json|csv]
aws_explorer glue runs <job>  [--since 7d] [--limit 20] [--status FAILED] [-o …]
aws_explorer glue crawlers    [--all-regions] [-o …]
aws_explorer glue triggers    [-o …]
aws_explorer glue workflows   [-o …]
```

```
$ aws_explorer glue runs nightly-orders-etl --since 3d -o table
STARTED              STATE       DURATION  DPU-HRS  EST     ERROR
2026-06-15 01:14:00  SUCCEEDED   12m22s    2.06     $0.91
2026-06-14 01:14:00  FAILED      2m41s     0.45     $0.20   AnalysisException: Table…not found
```

**Implementation.**
- Subcommands under the `glue` parent (`cmd/glue.go`), each collecting via the
  shared collector and rendering through `internal/output.StreamOutput` (table /
  JSON / NDJSON / CSV — no new format code). `glue` with no subcommand launches
  the dashboard (AXE-026).
- `--status` filters run state client-side; `--since` / `--limit` bound the run
  window.

**Acceptance criteria.**
- Each subcommand supports all four output formats; JSON includes
  machine-readable `dpuHours`, `estUSD`, ISO timestamps; `--status FAILED`
  returns only failures.

---

### AXE-031 — `glue` audit category {#axe-031}

> **Status: ✅ shipped (all 9 checks)** — `aws_explorer audit --only glue`
> (`internal/findings/glue.go` + `internal/audit/glue_collect.go`), wired into
> the category list so `--fail-on`/`--ignore`/SARIF all apply. Checks
> `GLU-JOB-001/002/003`, `GLU-CRAWL-001/002`, `GLU-COST-001/002`,
> `GLU-SEC-001` and `GLU-CONN-001` all land, each a pure function with
> positive+negative fixtures. `GLU-CONN-001` cross-references each connection's
> subnet/security-group against the region's live EC2 inventory and stays
> silent unless that inventory was gathered completely.

**Problem.** Broken or wasteful Glue is silent: a nightly job that's failed for a
week, a crawler stuck for hours, a job whose oversized worker config burns
DPU-hours for a two-minute run. These are deterministic and read-only detectable
— exactly the `audit` linter's wheelhouse.

**Checks** (each a pure function over a `GlueSnapshot`; stable check-IDs):

| ID | Finding | Detection | Severity |
|----|---------|-----------|----------|
| `GLU-JOB-001` | Job's last N runs all failed | `GetJobRuns` last 5 all `FAILED`/`TIMEOUT`/`ERROR` | 🔴 |
| `GLU-JOB-002` | Job never run / no run in 30 days | no run, or newest `StartedOn` > 30d | 🔵 |
| `GLU-JOB-003` | Latest run failed | newest run `FAILED`/`TIMEOUT` | 🟡 |
| `GLU-CRAWL-001` | Crawler's last crawl failed | `LastCrawl.Status == FAILED` | 🟡 |
| `GLU-CRAWL-002` | Crawler stuck running | `State == RUNNING` and `StartTime` > 6h | 🟡 |
| `GLU-COST-001` | Failed-run DPU-hour waste | sum DPU-hrs of failed runs in window × price | 🟡 (EstMonthlyUSD) |
| `GLU-COST-002` | Oversized workers vs runtime | many workers but `ExecutionTime` ≪ threshold | 🔵 |
| `GLU-SEC-001` | No security configuration | `SecurityConfiguration` unset (logs/S3/bookmark unencrypted) | 🟡 |
| `GLU-CONN-001` | Connection refs missing subnet/SG | `PhysicalConnectionRequirements` IDs not found | 🔵 |

**UX.**

```bash
aws_explorer audit --only glue
aws_explorer audit --only glue --fail-on warning -o sarif > glue.sarif
```

Renders the standard findings table; `--fail-on` / `--ignore` / SARIF all apply
via the existing CI plumbing (AXE-023).

**Implementation.**
- `internal/findings/glue.go`: `GlueSnapshot{Region, Jobs, Runs, Crawlers,
  Connections, completeness flags}` + `AnalyzeGlue(snap) []Finding`; check-ID
  constants; one file, one check per function, positive+negative fixture per
  check.
- Register the checks in `internal/findings/checks.go` and add `"glue"` to
  `auditCategories` in `cmd/audit.go`; collection lives in
  `internal/audit/glue_collect.go`, reusing the AXE-025 detailed pass.
- `GLU-COST-001/002` carry `EstMonthlyUSD` so they total alongside the other
  cost findings.

**Acceptance criteria.**
- Every check: pure function + fixture test (positive and negative).
- A denied `GetJobRuns` skips the run-dependent checks with a note, never fails
  the audit.
- Check IDs are stable and documented in the README's check table.

---

### AXE-032 — Glue console deep links {#axe-032}

> **Status: ✅ shipped** (#196) — `case "glue"` in `internal/consolelink` maps
> `job` (Glue Studio editor), `crawler`, `database`, `trigger`, `workflow` and
> `connection` to console URLs; ARN-search fallback for the rest. Table-tested.

**Problem.** Sometimes you need the console (the visual job editor, the DAG).
Generating the deep link is pure string work and saves a minute each time.

**UX.** `o` on any Glue resource in any TUI copies the console URL (and opens it
when local), as it already does for the 15+ mapped services.

**Implementation.** Add a `case "glue"` to `internal/consolelink`'s `deepLink()`
switch, by `Type`:

| Type | URL |
|------|-----|
| `job` | `https://<region>.console.aws.amazon.com/gluestudio/home?region=<r>#/editor/job/<name>/details` |
| `crawler` | `…/glue/home?region=<r>#/v2/data-catalog/crawlers/view/<name>` |
| `database` | `…/glue/home?region=<r>#/v2/data-catalog/databases/view/<name>` |
| `trigger` | `…/glue/home?region=<r>#/v2/etl-configuration/triggers/view/<name>` |
| `workflow` | `…/glue/home?region=<r>#/v2/etl-configuration/workflows/view/<name>` |

Unmapped types fall back to the existing Resource-Groups ARN search. Table-driven
tests: ARN/type in → URL out.

**Acceptance criteria.** Every Glue resource type yields a specific URL (or the
ARN-search fallback); region is interpolated correctly.

---

## Cross-cutting requirements

Per the roadmap's conventions, applied to every AXE item here:

1. **Permissions documented.** The README's Glue section lists the exact
   `glue:*` (and `logs:*`, `cloudwatch:*` for AXE-028/optional metrics) actions;
   any denial degrades that feature with a visible note, never a crash. The
   consolidated minimum read-only policy appendix gains the Glue actions.
2. **Read-only guarantee.** Nothing here mutates Glue. A future "run job now"
   would follow the cost-stating confirmation pattern and is explicitly out of
   scope for this document.
3. **Testing.** Every mapping/analysis/render function is pure and
   fixture-tested (the `messaging_test.go` / `pathtrace_test.go` convention);
   API wrappers stay thin and mockable.
4. **Config.** New tunables under existing sections (`glue.runWindow`,
   `glue.runLimit`, `audit.ignore`), with embedded defaults — works with zero
   config.
5. **Key bindings.** New TUI keys register through `internal/ui/keys.go` /
   `keyhints.go`; checked for per-screen collisions.
6. **Docs.** New guide `tui-glue.md` in `internal/docsgen/guides/` registered in
   `guideList`; a README feature section in the existing style; per-command
   reference pages auto-generated from the cobra tree.

---

## Phasing {#phasing}

Sized so each phase ships independently and is useful on its own:

| Phase | Items | Rationale |
|-------|-------|-----------|
| **1 — See the jobs** | AXE-025 (enrichment), AXE-026 (dashboard), AXE-027 (runs) | The headline: a working dashboard with run health and cost |
| **2 — Debug the jobs** | AXE-028 (logs), AXE-029 (definition), AXE-030 (CLI twins) | Close the "why did it fail / what's it configured as" loop; make it scriptable |
| **3 — Watch the jobs** | AXE-031 (audit category), AXE-032 (console links) | Turn it into a pipeline gate; polish navigation |

**Dependencies:** everything builds on AXE-025. AXE-027 needs the DPU-hour price
constant. AXE-031 reuses the AXE-025 detailed pass and the established
`internal/findings` + CI-mode platform (AXE-008 / AXE-023, already shipped).
AXE-028 reuses the `internal/cwtui` constructor and `internal/loggroup` (AXE-011,
shipped).
</content>
</invoke>
