# Amazon EMR Support — Design Specification

Status: **Proposed** · Theme K of the [enhancement roadmap](enhancement-roadmap.md)
(`AXE-001 … AXE-024`) · Tracking issue: _TBD_

This document specifies a first-class Amazon EMR feature set for `aws_explorer`,
spanning the EMR control plane **and** the big-data services that actually run on
an EMR cluster — **YARN** (resource negotiator / application scheduler),
**HBase** (the NoSQL store), and **Oozie** (the workflow scheduler).

It continues the roadmap's stable-ID scheme (`AXE-NNN`) under a new theme so the
work can be referenced unambiguously in commits, PRs and the tracking issue
(e.g. `AXE-041: HBase table browser`). Glue's Theme J ended at `AXE-032`;
EMR is **Theme K**, starting at `AXE-033`.

It follows the tool's established design principles:

1. **Deterministic, no AI** — every analysis is a pure function over data the
   EMR API (or an on-cluster REST API) returns, unit-testable with fixture
   snapshots (the pattern set by `internal/findings/messaging.go` and
   `internal/vpctui/findings.go`).
2. **Read-only by default** — the entire feature set only *describes* EMR;
   nothing starts, stops, terminates, resizes or submits to a cluster. Any
   future "submit this step" / "terminate cluster" action would follow the
   Reachability-Analyzer cost-stating confirmation pattern
   (`internal/vpctui/analyzer.go`) and is explicitly out of scope here.
3. **Best-effort collection** — a denied EMR API call, an unreachable cluster,
   or a disabled on-cluster daemon degrades a feature, never crashes it; partial
   results are kept and flagged (the `Collector` contract in
   `internal/services/service.go`).
4. **One UX language** — every new table/overlay/detail panel uses the shared
   theme/table/key-hint machinery in `internal/ui` and `internal/table`; findings
   render in the existing severity/resource/issue/fix style.

---

## The central architectural fact: control plane vs. on-cluster

EMR is unusual among the services this tool covers because the thing the user
cares about lives in **two different planes**:

| Plane | Examples | How you read it | Reachability |
|-------|----------|-----------------|--------------|
| **Control plane** (AWS API) | clusters, steps, instances, instance groups/fleets, bootstrap actions, security configurations, installed applications, release label | `emr:List*` / `emr:Describe*` — plain AWS API calls, same as every other collector | always (IAM only) |
| **History plane** (off-cluster, AWS-mediated) | Spark History Server, YARN Timeline Server, Tez UI; archived step/container/daemon logs | `emr:GetPersistentAppUIPresignedURL` (returns a browser link), and the S3 / CloudWatch **log archive** | always (IAM + S3 read) |
| **On-cluster plane** (live daemons) | **live YARN apps**, **HBase tables / row counts / region status**, **Oozie workflows & coordinators** | HTTP REST against daemons on the primary node — YARN RM `:8088`, HBase REST `:8080` / Master UI `:16010`, Oozie `:11000` | **only with network reachability** into the cluster's VPC (SSH tunnel / SOCKS proxy / VPN / direct) |

The first two planes fit the tool's existing "pure AWS-API, read-only, runs from
anywhere" model perfectly. The third plane — which is exactly where the user's
"see what HBase tables exist, browse them, record counts, status" request lands —
**has no AWS API**. AWS's own console gets there only by making you open an SSH
tunnel and a SOCKS proxy to the primary node
([HBase UI docs](https://docs.aws.amazon.com/emr/latest/ReleaseGuide/hbase-web-ui.html),
[HBase REST on :8080 docs](https://docs.aws.amazon.com/emr/latest/ReleaseGuide/emr-hbase-configure.html)).

So this design is **explicitly layered**: Phases 1–2 deliver the full control- and
history-plane experience with zero new infrastructure and the tool's usual
guarantees. Phase 3 introduces a single, opt-in **on-cluster connection layer**
and builds the live YARN / HBase / Oozie browsers on top of it — clearly fenced
off, clearly documented as requiring network reachability, and degrading to a
helpful "how to connect" message when it can't reach the daemon. This keeps the
tool honest: we never pretend an AWS API exists where it doesn't.

---

## Why EMR, and what's there today

Amazon EMR (Elastic MapReduce) is AWS's managed big-data platform — Hadoop, Spark,
HBase, Hive, Presto/Trino, Tez, Flink, Oozie and more, on managed EC2 (or EKS, or
Serverless). Engineers debug EMR from the console all day:

> "Is the cluster up, and how big is it?" · "Did last night's step succeed?" ·
> "Why did this step fail — where's the stderr?" · "Which YARN apps are running
> right now and who's hogging the queue?" · "What HBase tables exist, are their
> regions healthy, how many rows?" · "Did the Oozie coordinator fire?" · "How
> many normalized-instance-hours is this cluster burning?"

**Current state.** `internal/services/emr/collector.go` lists exactly **one**
resource type into the account-wide inventory — `cluster` — via `ListClusters`,
stamping only `State`, `NormalizedInstanceHours` and `CreatedAt`. It appears in
`summary`, the fuzzy finder, `whereused`, snapshot diffs and the console-link `o`
fallback. What's missing is **everything operational**: the cluster's hardware
and applications, its steps and their outcomes, its instances, its logs, the
history UIs, and any window at all into the YARN / HBase / Oozie services running
on it. This document closes that gap.

### Mapping EMR to the tool's surfaces

| EMR concept | API / source (read-only) | Where it lands |
|-------------|--------------------------|----------------|
| Cluster summary | `ListClusters` | inventory + dashboard (AXE-033, AXE-034) |
| Cluster detail (hw, apps, VPC, release) | `DescribeCluster` | detail panel (AXE-033, AXE-034) |
| Steps + outcome | `ListSteps` / `DescribeStep` | steps view (AXE-035) |
| Instances / fleets / groups | `ListInstances` / `ListInstanceFleets` / `ListInstanceGroups` | instances tab (AXE-034) |
| Bootstrap actions, security config | `ListBootstrapActions`, `ListSecurityConfigurations` / `DescribeSecurityConfiguration` | detail tabs (AXE-034) |
| Step / container / daemon logs | S3 log archive `s3://<bucket>/<cluster-id>/…` + CloudWatch | log jump (AXE-036) |
| Spark / YARN-timeline / Tez history | `GetPersistentAppUIPresignedURL` (SHS / YTS / TEZ) | history-UI link (AXE-037) |
| **Live YARN apps** | YARN RM REST `:8088/ws/v1/cluster/apps` | YARN browser (AXE-040) |
| **HBase tables / status / counts** | HBase REST `:8080` + Master status `:16010` | HBase browser (AXE-041) |
| **Oozie workflows / coordinators** | Oozie REST `:11000/oozie/v2/jobs` | Oozie browser (AXE-042) |
| Health / waste / security | derived from the above | `audit` category (AXE-043) |
| Deep links | pure string mapping | `o` in every EMR surface (AXE-044) |

---

## Contents

| ID | Title | Priority |
|----|-------|----------|
| [AXE-033](#axe-033) | EMR collector enrichment (cluster detail, steps, instances, apps) | **P1** |
| [AXE-034](#axe-034) | `emr` interactive dashboard TUI | **P1** |
| [AXE-035](#axe-035) | Step history & detail view (state · duration · failure reason) | **P1** |
| [AXE-036](#axe-036) | Jump from a cluster/step to its logs (S3 archive + CloudWatch) | **P2** |
| [AXE-037](#axe-037) | Persistent application-UI links (Spark / YARN-timeline / Tez) | **P2** |
| [AXE-038](#axe-038) | CLI twins (`emr clusters` / `steps` / `instances` / `apps`) | **P2** |
| [AXE-039](#axe-039) | On-cluster connection layer (opt-in SSH-tunnel / SOCKS / direct) | **P2** |
| [AXE-040](#axe-040) | YARN ResourceManager application browser (live) | **P3** |
| [AXE-041](#axe-041) | HBase table browser (namespaces · tables · status · counts) | **P3** |
| [AXE-042](#axe-042) | Oozie workflow & coordinator browser | **P3** |
| [AXE-043](#axe-043) | `emr` audit category (idle / failed / cost / security findings) | **P2** |
| [AXE-044](#axe-044) | EMR console deep links | **P3** |

Priorities: **P1** = build first, **P2** = next, **P3** = deferrable. A suggested
phasing is at the [end of this document](#phasing).

---

## Architecture context

Where the new code plugs in (all paths confirmed against the current tree):

- **Collector** — `internal/services/emr/collector.go` already exists and
  implements the `Collector` interface (`internal/services/service.go`); it is
  registered in the engine's `defaultRegistry()`
  (`internal/engine/engine.go`, alongside `glue.NewCollector()`). AXE-033 extends
  it with a `DetailLevelDetailed` pass and new resource types (`step`, `instance`,
  `instance-group`/`instance-fleet`), keeping the streaming `EmitOrAppend`
  page-batched pattern.
- **Engine** (`internal/engine`) fans the collector across regions with a bounded
  pool, keeps partial results, and aggregates errors — no change needed.
- **TUI** — the dashboard (AXE-034/035) is a new Bubble Tea package
  `internal/emrtui`, modelled on `internal/gluetui` (model/update/view split,
  `NewModel` factory, a `focus`/`tab` enum per pane, one `internal/table.Model`
  per tab, a `textinput.Model` filter, spinner + toast for async loads) and built
  from `internal/table` + `internal/ui` (themes, key hints, overlays, window
  titles). The command (`cmd/emr.go`) mirrors `cmd/glue.go`.
- **Log jump** (AXE-036) reuses the `internal/cwtui` constructor that accepts an
  initial group + stream filter (`NewModel(…, groupFilter, streamFilter,
  eventPattern)`) — the same mechanism the summary TUI's `L` key uses — and adds
  an EMR derivation to `internal/loggroup`. The S3 archive side reuses
  `internal/s3tui` to browse `s3://<logbucket>/<cluster-id>/…`.
- **History UIs** (AXE-037) call `emr:GetPersistentAppUIPresignedURL` and hand
  the URL to the existing "copy / open in browser" path used by `o`
  (`internal/consolelink` open helper).
- **On-cluster layer** (AXE-039) is a new `internal/emrconn` package: a thin HTTP
  client that, given a cluster's primary-node DNS and a connection mode, reaches
  the YARN / HBase / Oozie REST endpoints. It is the **only** new piece that
  reaches outside the AWS API surface, and is fully opt-in.
- **Findings** (AXE-043) add `internal/findings/emr.go` — an `EMRSnapshot` struct
  + pure `AnalyzeEMR(snap) []Finding`, check-ID constants, and registration in
  `internal/findings/checks.go`; the category is wired into `cmd/audit.go`'s
  `auditCategories` and `internal/audit/`.
- **Console links** (AXE-044) add a `case "emr"` to `internal/consolelink`'s
  `deepLink()` switch.
- **Output** (`internal/output`, `internal/csvexport`) renders the CLI twins
  (AXE-038) in table / JSON / NDJSON / CSV with zero new format code.
- **Docs** — a new guide `internal/docsgen/guides/tui-emr.md` registered in
  `docsgen.go`'s `guideList`; per-command reference pages auto-generate from the
  cobra tree.

### New / changed shared pieces

| Piece | Introduced by | Purpose |
|-------|---------------|---------|
| `internal/emrtui` | AXE-034/035/037/040/041/042 | Bubble Tea dashboard, modelled on `gluetui` |
| `internal/emrconn` | AXE-039 | Opt-in on-cluster HTTP client (YARN / HBase / Oozie REST) |
| `internal/findings/emr.go` | AXE-043 | `EMRSnapshot` + `AnalyzeEMR` pure checks |
| `internal/loggroup` EMR derivation | AXE-036 | cluster/step → S3 log prefix + CloudWatch group |
| `internal/costs` (extend) | AXE-035/043 | normalized-instance-hour / EC2-hour cost helper |
| `consolelink` `case "emr"` | AXE-044 | cluster / step / studio / notebook URLs |

---

## Theme K — Amazon EMR / big-data clusters

### AXE-033 — EMR collector enrichment {#axe-033}

**Problem.** The inventory knows a cluster *exists* and its state, but nothing
about what it *is* or what it's *doing*: how big it is, which applications
(Spark, HBase, Hive, Oozie…) are installed, what steps it has run, and how those
steps fared. The single most useful operational fact — "did last night's step
succeed?" — is absent. Steps and instances aren't collected at all.

**Scope.**
- **Cluster enrichment** at `DetailLevelSummary`+: one `DescribeCluster` per
  cluster to stamp the hardware and software picture into `Summary`:

  ```
  Summary["releaseLabel"]   = "emr-7.1.0"
  Summary["applications"]   = "Spark, HBase, Hive, Oozie"
  Summary["instanceCount"]  = "11"
  Summary["masterDNS"]      = "ip-10-0-1-23.ec2.internal"
  Summary["normalizedHrs"]  = "184"
  Summary["autoTerminate"]  = "false"
  ```

  `State` already carries the cluster status (`STARTING` / `BOOTSTRAPPING` /
  `RUNNING` / `WAITING` / `TERMINATING` / `TERMINATED` /
  `TERMINATED_WITH_ERRORS`); also stamp `Summary["stateChangeReason"]` from
  `Status.StateChangeReason` so a failed cluster explains itself.
- **New resource types** (added to the existing collector):
  - `step` — `ListSteps` per cluster: name, `Status.State` (`PENDING` /
    `RUNNING` / `COMPLETED` / `CANCELLED` / `FAILED` / `INTERRUPTED`),
    `ActionOnFailure`, timeline, and `Status.FailureDetails` (reason / message /
    log-location) for failures.
  - `instance` — `ListInstances` per cluster (guard behind `DetailLevelDetailed`;
    instances can be numerous): EC2 instance id, private/public DNS, instance
    type, market (ON_DEMAND / SPOT), state.
  - `instance-fleet` / `instance-group` — `ListInstanceFleets` /
    `ListInstanceGroups`: requested vs running counts, instance types, bid price.
- **Detailed pass** (`DetailLevelDetailed`) captures into `Details`: bootstrap
  actions (`ListBootstrapActions`), the security configuration name and (via
  `DescribeSecurityConfiguration`) whether at-rest / in-transit encryption is on,
  EC2 attributes (subnet, key name, instance profile), and the full applications
  list with versions.

**Implementation.**
- Extend `internal/services/emr/collector.go`. Each new listing is independent
  and best-effort: a failure in `collectSteps` is joined into the returned error
  (the existing `errors.Join` pattern) but never blocks cluster listing.
- Step / instance enrichment is one paginated call per cluster; cap concurrency
  with an `errgroup` exactly as `gluetui`'s `applyLatestRun()` does, and skip with
  a note if the action is denied (clusters still listed, no steps).
- Only enrich **non-terminated** clusters by default for the live picture, with
  a `--include-terminated` knob (terminated clusters' steps/instances are gone
  from the API after a retention window anyway).
- ARNs: clusters already return `ClusterArn`; synthesize step/instance ARNs in
  the Tagging-API form when absent, matching the existing `arn(...)` helper style.

**IAM needed.** `elasticmapreduce:ListClusters`, `DescribeCluster`, `ListSteps`,
`DescribeStep`, `ListInstances`, `ListInstanceFleets`, `ListInstanceGroups`,
`ListBootstrapActions`, `ListSecurityConfigurations`,
`DescribeSecurityConfiguration`.

**Acceptance criteria.**
- Clusters in `summary` show release label, app list and size at a glance; a
  `TERMINATED_WITH_ERRORS` cluster shows its state-change reason.
- A denied `ListSteps` lists clusters without steps and records one note, never a
  crash.
- New types (`step`, `instance`, `instance-fleet`) appear in `summary`, `find`,
  and JSON output with stable `Type` strings.

---

### AXE-034 — `emr` interactive dashboard TUI {#axe-034}

**Problem.** EMR's console spreads a single cluster across many tabs (Summary,
Application user interfaces, Monitoring, Hardware, Configurations, Steps, Events,
Bootstrap actions). One terminal dashboard that shows clusters, their steps,
their hardware and their installed apps side-by-side — with health at a glance —
is the headline feature this whole document builds toward.

**UX.**

```bash
aws_explorer emr                       # dashboard, all configured regions
aws_explorer emr --region us-east-1    # pin one region
aws_explorer emr --all-regions         # sweep + Region column
```

A tabbed Bubble Tea TUI (tab bar styled like the existing TUIs). Top level is the
**cluster list**; `enter` on a cluster opens its **per-cluster tabs**:
`Steps` · `Instances` · `Applications` · `Bootstrap` · `Configuration`.

```
 EMR ▸ Clusters                                                         us-east-1

  NAME                  ID            STATE         REL        APPS                  SIZE
▸ analytics-prod        j-1A2B3C4D5   ● WAITING     emr-7.1.0  Spark,HBase,Hive,…    11
  nightly-batch         j-9Z8Y7X6W    ● RUNNING     emr-7.1.0  Spark,Oozie           20
  adhoc-research        j-5F4E3D2C    ✓ TERMINATED  emr-6.15.0 Spark,Trino           (gone)
  ingest-legacy         j-0Q1W2E3R    ✗ TERM_W_ERR  emr-6.10.0 Spark,HBase           —
                        └ Step 'load-orders' failed: ActionOnFailure=TERMINATE_CLUSTER

  enter open · s steps · L logs · u app UIs · y yarn · h hbase · o console · / filter · ? help
```

State is colour-coded through the theme's role colours (success / error /
running / waiting / muted), the same vocabulary the audit and bill TUIs use.

**Keys** (registered via `internal/ui/keys.go` so the status bar stays truthful):
`tab` / `shift+tab` switch per-cluster panes · `enter` open the selected cluster
· `s` jump straight to its steps (AXE-035) · `L` jump to logs (AXE-036) · `u`
open a persistent application UI (AXE-037) · `y`/`h`/`z` open the live YARN /
HBase / Oozie browsers (AXE-040/041/042, when a connection is configured) · `o`
open in the console (AXE-044) · `/` filter · `r` refresh · `?` help · `q`/`esc`
back/quit.

**Implementation.**
- New `internal/emrtui` package and `cmd/emr.go`, mirroring `internal/gluetui` /
  `cmd/glue.go`: `NewModel(ctx, awsCfg, regions, allRegions, appCfg) (tea.Model,
  error)`, a `focus`/`tab` enum per pane, `internal/table.Model` per tab,
  `textinput.Model` for the filter, spinner + toast for async loads, and a
  `Client` (`internal/emrtui/client.go`) wrapping the collector + on-demand
  `DescribeCluster` / `ListSteps` calls.
- Data comes from the AXE-033 collector at `DetailLevelSummary`, loaded
  asynchronously per region; rows stream in as they arrive, with a
  `cached N · refreshing…` style status while loading.
- `SilenceScanLogs()` so the TUI owns the screen; `tea.WithAltScreen()` and
  `ui.WithWindowTitle` as the other TUIs do.

**Acceptance criteria.**
- Launch with zero args renders the cluster list from the configured regions;
  per-region load failures show in an errors overlay, not a crash.
- Every pane is filterable; `o` yields a valid console URL on every row.
- Tab switching preserves each pane's selection.

---

### AXE-035 — Step history & detail view {#axe-035}

**Problem.** "Did the step run, how long did it take, and why did it fail?" The
answer is in `ListSteps` / `DescribeStep` but buried behind console clicks, and
the failure reason + log pointer are the two things you always want first.

**UX.** `s`/`enter` on a cluster (dashboard) or `aws_explorer emr steps
<cluster-id>` opens the step list, newest first:

```
 EMR ▸ Steps — analytics-prod (j-1A2B3C4D5)                            us-east-1

  STARTED            STATE         DURATION   ACTION-ON-FAIL        NAME
▸ 2026-06-15 01:14   ✓ COMPLETED    18m 02s   CONTINUE              spark-submit nightly-orders
  2026-06-14 01:14   ✗ FAILED        2m 41s   TERMINATE_CLUSTER     spark-submit nightly-orders
                     └ FailureReason: Application application_… failed 2 times due to AM Container…
                       LogFile: s3://logs/j-1A2B3C4D5/steps/s-XYZ/stderr.gz
  2026-06-13 01:14   ✓ COMPLETED    17m 58s   CONTINUE              spark-submit nightly-orders

  enter detail · L logs · y copy reason · o console · esc back              3 steps
```

`enter` on a step opens a detail overlay: the full `HadoopJarStep`
(jar / mainClass / args), `ActionOnFailure`, timeline, and
`Status.FailureDetails` (reason · message · `LogFile`). A failed row expands its
reason inline.

**Implementation.**
- `internal/emrtui` steps view backed by `ListSteps` (paginated), with a
  configurable window (default last 50 steps / 7 days, `--since`/`--limit` on the
  CLI twin).
- The step → row mapping and the failure-detail render are pure functions over
  `[]types.StepSummary` / `*types.Step`, fixture-tested
  (completed / failed-with-reason / running / cancelled / interrupted cases).
- Duration derives from `Status.Timeline` (`StartDateTime` → `EndDateTime`);
  running steps show no duration.

**IAM needed.** `elasticmapreduce:ListSteps`, `DescribeStep`.

**Acceptance criteria.**
- Steps sorted newest-first; failed steps show their reason and log location
  inline; running steps show no duration.
- A cluster with no steps shows "no steps", not an empty table.

---

### AXE-036 — Jump from a cluster/step to its logs {#axe-036}

**Problem.** The next click after "it failed" is always "show me the logs". EMR
writes them to two predictable places: an **S3 log archive** (when a log URI is
configured on the cluster) and, optionally, **CloudWatch**.

**UX.** `L` on a cluster (its log root) or on a step (its step folder):
- If the cluster has an S3 `LogUri`, open `internal/s3tui` rooted at the derived
  prefix so the user can browse straight to the gzipped log they need.
- Reuse the `internal/cwtui` constructor (pre-filtered group/stream) when the
  cluster publishes to CloudWatch.
- `esc`/`q` returns to where you were (the AXE-011 round-trip guarantee).

EMR's S3 archive layout (under the cluster's `LogUri`):

| Prefix | Contents |
|--------|----------|
| `<cluster-id>/steps/<step-id>/` | `controller`, `stderr`, `stdout`, `syslog` (per step) |
| `<cluster-id>/node/<instance-id>/` | per-node daemon logs (incl. HBase, instance-controller) |
| `<cluster-id>/containers/<app-id>/` | YARN container logs (Spark executors etc.) |
| `<cluster-id>/hadoop-mapreduce/` | history-server / job-history files |

**Implementation.**
- Add an EMR derivation to `internal/loggroup`:
  `emrStepLog(cluster, step) (s3prefix string, cwGroup string, ok bool)` — pure,
  table-tested. The S3 prefix is `parse(LogUri)/<cluster-id>/steps/<step-id>/`.
- The S3 side hands the prefix to the existing `internal/s3tui` constructor; the
  CloudWatch side reuses the `cwtui` group/stream filter constructor. Off a TTY,
  degrade to a toast printing the S3 URI / group name.
- When the cluster has no `LogUri` and no CloudWatch logging, show a toast saying
  so (a common misconfiguration worth surfacing — see AXE-043).

**IAM needed.** `s3:ListBucket` / `s3:GetObject` on the log bucket; the cw TUI's
existing `logs:*` read actions.

**Acceptance criteria.**
- `L` on a step lands in the S3 browser at that step's folder when a `LogUri`
  exists; falls back to a clear "no log destination configured" toast otherwise;
  round-trip preserves the steps-view selection.

---

### AXE-037 — Persistent application-UI links {#axe-037}

**Problem.** The Spark History Server, YARN Timeline Server and Tez UI are the
canonical way to debug a finished Spark/Tez/YARN job — but on a live cluster they
need an SSH tunnel, and after the cluster dies they're gone. AWS solves this with
**persistent (off-cluster) application UIs**, reachable via a presigned URL that
needs no tunnel and survives 30 days past application termination
([docs](https://docs.aws.amazon.com/emr/latest/ManagementGuide/app-history-spark-UI.html)).

**UX.** `u` on a cluster opens a small picker — `Spark History` (SHS) ·
`YARN Timeline` (YTS) · `Tez` — and on selection calls
`GetPersistentAppUIPresignedURL`, then copies/opens the resulting URL the same
way `o` does for console links. Off a TTY, print the URL.

**Implementation.**
- `internal/emrtui` action calling, in order, `CreatePersistentAppUI` (idempotent
  per cluster) → `DescribePersistentAppUI` (poll until `ATTACHED`/ready) →
  `GetPersistentAppUIPresignedURL` with `PersistentAppUIType` ∈ {`SHS`, `YTS`,
  `TEZ`}. The presigned URL is opened via the existing browser-open helper.
- Best-effort: if the persistent-UI actions are denied, fall back to the console
  "Application user interfaces" deep link (AXE-044) and a note.

**IAM needed.** `elasticmapreduce:CreatePersistentAppUI`,
`DescribePersistentAppUI`, `GetPersistentAppUIPresignedURL` (and, for one-click
on-cluster UIs on a live cluster, `GetOnClusterAppUIPresignedURL`).

**Acceptance criteria.**
- `u` → Spark History yields a working presigned URL for a cluster that ran
  Spark; a denial degrades to the console link + a toast, never a crash.

---

### AXE-038 — CLI twins {#axe-038}

**Problem.** Everything the dashboard shows should be scriptable and pipeable —
the tool's standing contract (every TUI has a CLI twin).

**UX.**

```bash
aws_explorer emr clusters    [--all-regions] [--state RUNNING,WAITING] [-o table|json|csv]
aws_explorer emr steps <id>  [--since 7d] [--limit 50] [--status FAILED] [-o …]
aws_explorer emr instances <id>  [-o …]
aws_explorer emr apps <id>       [-o …]     # installed applications + versions
```

```
$ aws_explorer emr steps j-1A2B3C4D5 --status FAILED -o table
STARTED              STATE     DURATION  ACTION-ON-FAIL     REASON
2026-06-14 01:14:00  FAILED    2m41s     TERMINATE_CLUSTER  Application application_… failed 2 times…
```

**Implementation.**
- Subcommands under the `emr` parent (`cmd/emr.go`), each collecting via the
  shared `internal/emrtui.Client` and rendering through
  `internal/output.StreamOutput` (table / JSON / NDJSON / CSV — no new format
  code). `emr` with no subcommand launches the dashboard (AXE-034).
- `--status` / `--state` filter client-side; `--since` / `--limit` bound the step
  window. JSON exposes machine-readable `durationSeconds`, ISO timestamps and the
  failure `logFile`.

**Acceptance criteria.**
- Each subcommand supports all four output formats; `--status FAILED` returns
  only failed steps; `emr clusters --state RUNNING,WAITING` filters by state.

---

### AXE-039 — On-cluster connection layer {#axe-039}

**Problem.** YARN's live application list, HBase's tables, and Oozie's workflows
have **no AWS API** — they live on REST daemons on the cluster's primary node,
reachable only from inside the VPC. To browse them (AXE-040/041/042) the tool
needs a way to reach those daemons. This is the one place the tool steps outside
the pure AWS-API model, so it is **opt-in, explicit, and read-only**.

**Design.** A new `internal/emrconn` package: a thin HTTP client that resolves a
cluster's primary-node private DNS (from `DescribeCluster`) and reaches the
on-cluster REST endpoints through one of three **connection modes**, selected in
config or per-invocation:

| Mode | How | When to use |
|------|-----|-------------|
| `direct` | HTTP straight to `http://<primary-dns>:<port>` | the tool runs inside the VPC (bastion, CloudShell-in-VPC, peered network) |
| `socks` | route through an existing SOCKS5 proxy (e.g. an `ssh -D 8157` tunnel the user already runs — the exact pattern AWS documents for the web UIs) | local laptop with an SSH dynamic tunnel |
| `tunnel` | the tool opens its own SSH local-forward to `<port>` using a user-supplied key + bastion/primary host | one-shot, no pre-existing proxy |

Config (embedded defaults, zero-config still works for Phases 1–2):

```yaml
emr:
  onCluster:
    mode: socks            # off | direct | socks | tunnel
    socksProxy: 127.0.0.1:8157
    ssh:
      user: hadoop
      keyFile: ~/.ssh/emr.pem
    ports:                 # EMR defaults; overridable per cluster
      yarn:  8088
      hbase: 8080          # HBase REST server
      oozie: 11000
    timeout: 5s
```

Default is `off`: the live browsers are dark until the user opts in, and each
shows a one-screen "how to connect" helper (the SSH-tunnel command, the security
group note) when `mode: off` or a connection fails.

**Implementation.**
- `emrconn.Dialer` builds an `*http.Client` per mode (`direct` = default
  transport; `socks` = `golang.org/x/net/proxy` SOCKS5 dialer; `tunnel` = an
  `golang.org/x/crypto/ssh` local-forward established for the session and torn
  down on exit). All three are read-only HTTP GETs against REST endpoints.
- Every request is wrapped with the config `timeout` and returns a typed
  `ErrUnreachable` that the browsers render as the connect-helper screen rather
  than an error toast.
- No credentials are persisted; SSH keys are read from the user-specified path
  only.

**Security note.** This layer can reach private infrastructure, so it is
strictly opt-in, never default, logs the endpoint it dials, and performs only
HTTP `GET`. It is documented in the README with the security-group / tunnel
caveats AWS itself states for these UIs.

**Acceptance criteria.**
- With `mode: off`, AXE-040/041/042 render the connect helper, never an error.
- With a working `socks` proxy, a GET to the YARN RM endpoint returns parsed JSON.
- A timeout / refused connection degrades to the connect helper, never a crash.

---

### AXE-040 — YARN ResourceManager application browser {#axe-040}

**Problem.** "What's running on the cluster *right now*, and who's eating the
queue?" is a live question the EMR API can't answer — it's YARN's job. The YARN
ResourceManager exposes it over REST at `:8088/ws/v1/cluster/apps`.

**UX.** `y` on a cluster (when `emrconn` is configured) opens the YARN apps view:

```
 EMR ▸ YARN — analytics-prod (j-1A2B3C4D5)        via socks 127.0.0.1:8157

  APPLICATION              STATE      FINAL    PROGRESS  QUEUE     USER     ELAPSED
▸ application_…_0042       RUNNING    UNDEF      63%     default   hadoop   12m
  application_…_0041       FINISHED   SUCCEEDED 100%     default   hadoop   18m
  application_…_0039       FAILED     FAILED    100%     adhoc     analyst  2m

  enter detail · u app UI · / filter · r refresh · esc back        cluster: 184/256 GB used
```

Columns from the RM `apps` payload: `id`, `state`, `finalStatus`,
`progress`, `queue`, `user`, `applicationType`, elapsed (from `startedTime`).
A cluster-metrics footer (`/ws/v1/cluster/metrics`) shows memory/vcores used vs
total. `enter` opens an app-detail overlay; `u` cross-links to the persistent
Spark/Tez UI for that app (AXE-037).

**Implementation.**
- `internal/emrtui` YARN view backed by `emrconn` GETs to
  `/ws/v1/cluster/apps` and `/ws/v1/cluster/metrics`; the JSON → row mapping is a
  pure, fixture-tested function over recorded RM responses (running / finished /
  failed / killed cases).
- `r` re-fetches (live data); a fetch failure swaps the table for the connect
  helper.

**IAM needed.** none (on-cluster); requires AXE-039 reachability.

**Acceptance criteria.**
- With a reachable RM, running and finished apps render with progress and queue;
  the metrics footer is correct; `r` refreshes live.
- With no connection, the view is the connect helper, not an error.

---

### AXE-041 — HBase table browser {#axe-041}

> This is the feature the request centred on: "see what HBase tables exist,
> browse them, record counts, status."

**Problem.** HBase on EMR has no AWS API. Its tables, their region health and
their sizes live on the HBase cluster itself — exposed by the **HBase REST
server** on `:8080` and the **Master status UI** on `:16010`
([EMR HBase UI docs](https://docs.aws.amazon.com/emr/latest/ReleaseGuide/hbase-web-ui.html)).

**UX.** `h` on a cluster (when `emrconn` is configured and HBase is installed)
opens the HBase browser. Top level lists **namespaces → tables**:

```
 EMR ▸ HBase — analytics-prod (j-1A2B3C4D5)            via socks 127.0.0.1:8157

  NAMESPACE   TABLE              STATE      REGIONS  ONLINE  CFAMILIES   APPROX ROWS
▸ default     orders             ENABLED      24       24    cf,meta     ~ 4.2 M
  default     customers          ENABLED       8        8    cf          ~ 980 K
  staging     orders_tmp         DISABLED      4        0    cf          —
  default     clickstream        ENABLED     128      126    cf          ~ 311 M  ⚠ 2 regions in transition

  enter schema · c count rows · o console · / filter · r refresh · esc back
```

What each column comes from:

| Column | Source (HBase REST / Master) |
|--------|------------------------------|
| Namespace / Table | `GET /namespaces`, `GET /namespaces/<ns>/tables` |
| State (ENABLED / DISABLED) | table `/schema` + Master status (`IS_META`/enabled flag) |
| Regions / Online | `GET /<table>/regions` (region list) vs Master `regionsInTransition` |
| Column families | `GET /<table>/schema` (`ColumnSchema` list) |
| **Approx rows** | see note below |

**On record counts — being honest.** HBase has *no* cheap exact row count.
Options, surfaced as an explicit, opt-in `c` action rather than computed eagerly:
- **Default (cheap, always shown):** region count + approximate store size from
  the Master status / `/<table>/regions` — labelled "approx", never presented as
  exact.
- **`c` count rows (explicit, may be slow):** issue a scan with the
  `org.apache.hadoop.hbase.mapreduce.RowCounter`-equivalent over REST, or a
  filtered `KeyOnlyFilter` scan, **clearly warning** it scans the table. This is
  read-only but not free, so it follows the cost-stating confirmation pattern
  (the analyzer precedent) before running.

`enter` opens a table-schema overlay (column families, TTL, compression, BLOOM,
versions). Region-in-transition / split counts drive a ⚠ status flag and an
audit finding (AXE-043).

**Implementation.**
- `internal/emrtui` HBase view backed by `emrconn` GETs to the HBase REST server
  (`:8080`) for namespaces/tables/regions/schema, and the Master status JSON
  (`:16010/master-status?format=json` where available, else scrape the status
  page) for region-in-transition and online counts.
- All HBase JSON → row mappings are pure, fixture-tested functions over recorded
  REST responses (enabled / disabled / regions-in-transition / multi-family
  cases). The row-count scan is guarded behind the confirmation gate and a hard
  timeout.
- If HBase isn't in the cluster's application list, `h` is disabled with a hint.

**IAM needed.** none (on-cluster); requires AXE-039 reachability and the HBase
REST server running (default on recent EMR releases; the security group must
allow `:8080` — surfaced in the connect helper and as an audit finding).

**Acceptance criteria.**
- With a reachable HBase REST server, namespaces and tables list with state,
  region/online counts and column families; disabled tables show 0 online.
- `c` prompts before scanning and reports an approximate or exact count with a
  clear label; a slow/denied scan times out into a toast, never hangs the UI.
- Without HBase installed or reachable, the view is the connect/"not installed"
  helper, not an error.

---

### AXE-042 — Oozie workflow & coordinator browser {#axe-042}

**Problem.** Oozie is EMR's workflow scheduler (`o-o-z-i-e`), and "did the
coordinator fire and did the workflow succeed?" is a daily question with, again,
no AWS API — only the Oozie REST server on the primary node (`:11000`).

**UX.** `z` on a cluster (when `emrconn` is configured and Oozie is installed)
opens the Oozie browser with two tabs — `Workflows` and `Coordinators`:

```
 EMR ▸ Oozie — nightly-batch (j-9Z8Y7X6W) ▸ Coordinators    via socks 127.0.0.1:8157

  NAME                 STATUS     FREQUENCY   LAST MATERIALIZED     NEXT          RUNNING
▸ orders-hourly        RUNNING    60 min      2026-06-15 19:00      20:00            1
  dim-refresh-daily    SUSPENDED  1440 min     2026-06-15 02:00      —                0
  legacy-export        KILLED     1440 min     2026-06-10 02:00      —                0

  tab workflows · enter actions · / filter · r refresh · esc back
```

Sources: `GET /oozie/v2/jobs?jobtype=wf` (workflows: status SUCCEEDED / RUNNING /
KILLED / SUSPENDED / FAILED, start/end, app name) and `?jobtype=coordinator`
(coordinators: status, frequency, last-materialized, next-materialized,
running-count). `enter` on a workflow lists its **actions** (each action's name,
type, status, transition) — the usual "which step of the DAG broke" view.

**Implementation.**
- `internal/emrtui` Oozie view backed by `emrconn` GETs to `/oozie/v2/jobs` and
  `/oozie/v2/job/<id>`; pure, fixture-tested JSON → row mappings (succeeded /
  running / killed / suspended; coordinator with/without next-materialization).
- If Oozie isn't installed, `z` is disabled with a hint.

**IAM needed.** none (on-cluster); requires AXE-039 reachability.

**Acceptance criteria.**
- With a reachable Oozie server, workflows and coordinators list with status and
  schedule; `enter` shows a workflow's actions and which one failed.
- Without Oozie installed or reachable, the view is the connect/"not installed"
  helper, not an error.

---

### AXE-043 — `emr` audit category {#axe-043}

**Problem.** Broken or wasteful EMR is expensive and silent: a cluster left
`WAITING` idle for days, a cluster that died with errors, a step that's failed
every night for a week, a cluster with no log destination (so nobody can debug
it), or one with encryption / public-access misconfigurations. These are
deterministic and read-only detectable — exactly the `audit` linter's wheelhouse.

**Checks** (each a pure function over an `EMRSnapshot`; stable check-IDs):

| ID | Finding | Detection | Severity |
|----|---------|-----------|----------|
| `EMR-COST-001` | Idle cluster | `State == WAITING` with no `RUNNING` step for > N hours | 🟡 (EstMonthlyUSD) |
| `EMR-COST-002` | Long-running cluster | non-auto-terminating cluster up > 7 days | 🔵 (EstMonthlyUSD) |
| `EMR-COST-003` | Auto-termination disabled | `AutoTerminationPolicy` unset on a transient-looking cluster | 🔵 |
| `EMR-STEP-001` | Latest step failed | newest step `FAILED` / `CANCELLED` | 🟡 |
| `EMR-STEP-002` | Cluster terminated with errors | `State == TERMINATED_WITH_ERRORS` | 🔴 |
| `EMR-LOG-001` | No log destination | cluster has no `LogUri` and no CloudWatch logging | 🟡 |
| `EMR-SEC-001` | No security configuration / encryption off | no security config, or at-rest/in-transit encryption disabled | 🟡 |
| `EMR-SEC-002` | Primary node publicly reachable | EC2 attrs put the primary in a public subnet with an open SG on a UI/REST port | 🔴 |
| `EMR-HBASE-001` | HBase regions stuck in transition | (with `emrconn`) `regionsInTransition` > 0 for > N min | 🟡 |

`EMR-HBASE-001` requires the on-cluster layer; it's **skipped with a note** when
`emrconn` is `off` or unreachable, exactly as denied-API checks skip.

**UX.**

```bash
aws_explorer audit --only emr
aws_explorer audit --only emr --fail-on warning -o sarif > emr.sarif
```

Renders the standard findings table; `--fail-on` / `--ignore` / SARIF all apply
via the existing CI plumbing (AXE-023).

**Implementation.**
- `internal/findings/emr.go`: `EMRSnapshot{Region, Clusters, Steps, SecurityConfigs,
  HBaseStatus, completeness flags}` + `AnalyzeEMR(snap) []Finding`; check-ID
  constants; one check per function, positive + negative fixture per check.
- Register the checks in `internal/findings/checks.go` and add `"emr"` to
  `auditCategories` in `cmd/audit.go`; collection lives in
  `internal/audit/emr_collect.go`, reusing the AXE-033 detailed pass.
- Cost checks carry `EstMonthlyUSD` (from a normalized-instance-hour / EC2-hour
  helper in `internal/costs`) so they total alongside the other cost findings.

**Acceptance criteria.**
- Every check: pure function + fixture test (positive and negative).
- A denied `ListSteps` or `emrconn: off` skips the dependent checks with a note,
  never fails the audit.
- Check IDs are stable and documented in the README's check table.

---

### AXE-044 — EMR console deep links {#axe-044}

**Problem.** Sometimes you need the console (the visual cluster page, the
Application-UIs tab, EMR Studio). Generating the deep link is pure string work
and saves a minute each time.

**UX.** `o` on any EMR resource in any TUI copies the console URL (and opens it
when local), as it already does for the 15+ mapped services.

**Implementation.** Add a `case "emr"` to `internal/consolelink`'s `deepLink()`
switch, by `Type`:

| Type | URL |
|------|-----|
| `cluster` | `https://<region>.console.aws.amazon.com/emr/home?region=<r>#/clusterDetails/<cluster-id>` |
| `step` | `…/emr/home?region=<r>#/clusterDetails/<cluster-id>?step=<step-id>` (cluster page, Steps tab) |
| `app-ui` | `…/emr/home?region=<r>#/clusterDetails/<cluster-id>/applicationUserInterfaces` |
| `studio` | `…/emr/home?region=<r>#/studio/<studio-id>` |
| `notebook` | `…/emr/home?region=<r>#/notebooks/<notebook-id>` |

Unmapped types fall back to the existing Resource-Groups ARN search. Table-driven
tests: ARN/type in → URL out.

**Acceptance criteria.** Every EMR resource type yields a specific URL (or the
ARN-search fallback); region and cluster-id are interpolated correctly.

---

## Cross-cutting requirements

Per the roadmap's conventions, applied to every AXE item here:

1. **Permissions documented.** The README's EMR section lists the exact
   `elasticmapreduce:*` (and `s3:*`, `logs:*` for AXE-036; the persistent-UI
   actions for AXE-037) actions; any denial degrades that feature with a visible
   note, never a crash. The consolidated minimum read-only policy appendix gains
   the EMR actions.
2. **Read-only guarantee.** Nothing here mutates EMR or a cluster. Every
   on-cluster call (AXE-039–042) is an HTTP `GET`; the one potentially expensive
   read — HBase row counting (AXE-041) — is gated behind an explicit
   cost-stating confirmation. "Submit step" / "terminate cluster" / "resize" are
   explicitly out of scope.
3. **On-cluster reachability is opt-in.** AXE-039's connection layer is `off` by
   default; the live browsers render a connect helper until configured, and never
   surface a private-network failure as a crash. The security implications
   (reaching into a VPC, SSH keys, security-group ports) are documented.
4. **Testing.** Every mapping/analysis/render function is pure and
   fixture-tested (the `messaging_test.go` / `format_test.go` convention),
   including recorded YARN / HBase / Oozie REST payloads; API and HTTP wrappers
   stay thin and mockable.
5. **Config.** New tunables under an `emr` section (`emr.stepWindow`,
   `emr.includeTerminated`, the `emr.onCluster.*` block, `audit.ignore`), with
   embedded defaults — Phases 1–2 work with zero config.
6. **Key bindings.** New TUI keys (`s`/`u`/`y`/`h`/`z`/`c`) register through
   `internal/ui/keys.go` / `keyhints.go`; checked for per-screen collisions.
7. **Docs.** New guide `tui-emr.md` in `internal/docsgen/guides/` registered in
   `guideList`; a README feature section in the existing style; per-command
   reference pages auto-generated from the cobra tree.

---

## Phasing {#phasing}

Sized so each phase ships independently and is useful on its own:

| Phase | Items | Rationale |
|-------|-------|-----------|
| **1 — See the clusters** | AXE-033 (enrichment), AXE-034 (dashboard), AXE-035 (steps) | The headline: a working cluster dashboard with step health, pure AWS API, runs from anywhere |
| **2 — Debug the jobs** | AXE-036 (logs), AXE-037 (app UIs), AXE-038 (CLI twins), AXE-043 (audit) | Close the "why did it fail / show me the history" loop; make it scriptable and a CI gate — still pure AWS API |
| **3 — Into the cluster** | AXE-039 (connection layer), AXE-040 (YARN), AXE-041 (HBase), AXE-042 (Oozie) | The opt-in on-cluster browsers — live YARN apps, HBase tables, Oozie workflows |
| **4 — Polish** | AXE-044 (console links) | Navigation polish (can land any time; small) |

**Dependencies:** everything in Phases 1–2 builds on AXE-033. AXE-036 reuses
`internal/loggroup` + `internal/s3tui` + `internal/cwtui` (AXE-011 precedent).
AXE-043 reuses the AXE-033 detailed pass and the established `internal/findings` +
CI-mode platform (AXE-008 / AXE-023, shipped). **Phase 3 is gated entirely on
AXE-039** — the connection layer is the foundation for the YARN / HBase / Oozie
browsers, and the single point where the tool, deliberately and opt-in, reaches
beyond the AWS API into the cluster itself.
</content>
</invoke>
