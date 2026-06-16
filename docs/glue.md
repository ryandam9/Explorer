# AWS Glue dashboard

`glue` opens an interactive dashboard for AWS Glue. Tab across **Jobs**,
**Crawlers**, **Triggers**, **Workflows**, **Connections** and the **Catalog**
(databases); each row shows health at a glance — a job's last run state and
duration, a crawler's last-crawl status. Press **Enter** on a job to drill into
its **run history**: state, duration, DPU-hours and an estimated cost per run,
with the error message inline on failures. On the other tabs, **Enter** opens a
**detail overlay** for the selected crawler, trigger, workflow, connection or
database — its configuration, targets/actions and last-run/last-crawl status,
fetched on demand.

```bash
./bin/aws_explorer glue [--region us-east-1 | --all-regions] [--theme <name>]
```

```
 Glue ▸ Jobs (4)  Crawlers (2)  Triggers (3)  Workflows (1)  Connections (2)  Catalog (5)

 NAME                  LAST RUN          STATE         DURATION   WORKER      VERSION
 nightly-orders-etl    2026-06-15 01:14  ✓ SUCCEEDED   12m 22s    G.1X ×10    4.0
 customer-dedupe       2026-06-15 01:14  ✗ FAILED      2m 41s     G.2X ×5     4.0
 clickstream-flatten   2026-06-14 22:00  ● RUNNING     —          G.1X ×20    4.0
```

Run history (Enter on a job):

```
 Runs — nightly-orders-etl [us-east-1]
 STARTED           STATE         DURATION   DPU-HRS  EST     WORKER      ATTEMPT
 2026-06-15 01:14  ✓ SUCCEEDED   12m 22s    2.06     $0.91   G.1X ×10    1
 2026-06-14 01:14  ✗ FAILED      2m 41s     0.45     $0.20   G.1X ×10    1
   ✗ AnalysisException: Table or view not found: orders_raw
                                          3 runs · 4.50 DPU-hrs ≈ $1.99 (estimate)
```

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Switch pane |
| `↑/↓` (`j/k`) | Move selection |
| `Enter` | On **Jobs**, open the selected job's run history; on the other tabs, open the selected resource's detail overlay (fetched on demand) |
| `d` | Show the selected job's definition (role, version, worker, script, connections, args — secrets redacted); the overlay scrolls (`↑/↓`) when taller than the screen |
| `f` | **Findings** — deterministic posture/cost checks (failing or stale jobs, failed crawls) over the loaded jobs & crawlers; `y` copies the suggested fix |
| `S` / `R` | Cycle the active tab's sort column / reverse the direction (resets on a tab switch) |
| `/` | Filter the current pane |
| `o` | Open the selected resource in the AWS console |
| `r` | Refresh |
| `L` | (run history) open the selected run's CloudWatch logs (`/aws-glue/jobs/*`, stream = run ID) |
| `y` | (run history) copy the selected run's error |
| `i` | About this page · `q` quit |

The **Findings** panel reuses the same deterministic checks as `audit`
(`GLU-JOB-*`, `GLU-CRAWL-001`) over the data already on screen — no extra AWS
calls. Checks that need data the dashboard doesn't load (a job's security
configuration, worker count, per-run DPU-seconds, the VPC network inventory) are
**suppressed rather than guessed**; run `audit` for the full set.

The DPU-hour cost is an **estimate** (`$0.44`/DPU-hour, us-east-1 ETL rate);
runs that report no `DPUSeconds` (still running, or legacy jobs) show no figure
rather than `$0.00`. Glue DPU pricing varies by region, so for a job outside
us-east-1 the run-history footer flags that the figure uses the us-east-1 rate
(`estimate · us-east-1 rate`).

### Scriptable twins

Every pane has a non-interactive command for pipelines and `jq`:

```bash
aws_explorer glue jobs       [--all-regions] [-o table|json|ndjson|csv]
aws_explorer glue crawlers   [-o …]
aws_explorer glue triggers   [-o …]
aws_explorer glue workflows  [-o …]
aws_explorer glue runs <job> [-r us-east-1] [--limit 20] [--status FAILED] [-o …]
```

```bash
# Which jobs failed their last run?
aws_explorer glue jobs -o json | jq '[.[] | select(.lastRunState=="FAILED") | .name]'

# Failed runs of one job, with cost
aws_explorer glue runs nightly-orders-etl --status FAILED -o json | jq '.[] | {started, estUsd}'
```

The runs JSON exposes machine-readable `durationSeconds`, `dpuHours`, `estUsd`
and ISO-8601 `started`/`completed`. `runs` is region-specific: it uses
`--region` when given, otherwise the first region in scope.

**IAM permissions.** Read-only:
`glue:{GetJobs,GetJob,GetJobRuns,GetCrawlers,GetCrawler,GetTriggers,GetTrigger,ListWorkflows,GetWorkflow,GetConnections,GetConnection,GetDatabases,GetDatabase}`
and `sts:GetCallerIdentity` (for ARN/console links). The detail overlay's
`Get*` calls and any per-region or per-listing denial degrade just that part of
the dashboard with a logged note and never abort the session.
