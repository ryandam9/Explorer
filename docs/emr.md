# Amazon EMR dashboard

`emr` opens an interactive dashboard for Amazon EMR. Each row is a **cluster**,
colour-coded by state, showing its release label, the applications installed on
it (Spark, HBase, Hive, Oozie…), its size and how long ago it was created
(**AGE**). Press **Enter** (or `s`) on a
cluster to drill into its **step history** — state, duration and
action-on-failure, with the failure reason inline on a failed step. Press `d`
to **describe** the cluster in full — configuration, OS, compute layout (with
per-instance memory, vCPU and EBS storage), running EC2 instances, installed
services and VPC networking (subnet, security-group rules, routes, network ACL).

```bash
./bin/aws_explorer emr [--region us-east-1 | --all-regions] [--theme <name>]
```

```
 EMR ▸ Clusters

 NAME              ID           STATE                  RELEASE     APPLICATIONS           HRS
 analytics-prod    j-1A2B3C4D5  ● WAITING              emr-7.1.0   Spark, HBase, Hive…    184
 nightly-batch     j-9Z8Y7X6W   ● RUNNING              emr-7.1.0   Spark, Oozie            20
 ingest-legacy     j-0Q1W2E3R   ✗ TERMINATED_WITH_ERR  emr-6.10.0  Spark, HBase            —
   └ Step 'load-orders' failed: ActionOnFailure=TERMINATE_CLUSTER
```

Step history (Enter on a cluster):

```
 Steps — analytics-prod [us-east-1]
 STARTED           STATE         DURATION   ACTION-ON-FAIL      NAME
 2026-06-15 01:14  ✓ COMPLETED   18m 02s    CONTINUE            spark-submit nightly-orders
 2026-06-14 01:14  ✗ FAILED      2m 41s     TERMINATE_CLUSTER   spark-submit nightly-orders
   ✗ Application application_… failed 2 times due to AM Container…
     log: s3://logs/j-1A2B3C4D5/steps/s-XYZ/stderr.gz
```

| Key | Action |
|-----|--------|
| `↑/↓` (`j/k`) | Move selection |
| `Enter` / `s` | Open the selected cluster's step history |
| `d` | **Describe** the selected cluster — a full-screen, btop-style grid of panels (overview, configuration & OS, services, compute/memory/storage, EC2 instances, VPC networking, configurations). `Tab`/`Shift+Tab` (or `←/→`) move focus between panels; the focused panel scrolls (`↑/↓`, `PgUp/PgDn`, `g/G`). `Esc` closes. On a small terminal it collapses to one scrolling pane |
| `f` | **Findings** — deterministic posture/cost checks (idle/long-running clusters, no log destination or security config, terminated-with-errors) over the loaded clusters; `y` copies the suggested fix |
| `L` | Open the cluster's (or selected step's) logs in the S3 browser |
| `u` | Open a persistent application UI (Spark History / YARN Timeline / Tez) |
| `y` | Open the live **YARN application browser** (requires on-cluster access) |
| `b` | Open the **HBase table browser** (requires on-cluster access) |
| `z` | Open the **Oozie workflow/coordinator browser** (requires on-cluster access) |
| `t` | Toggle the **terminated** cluster tail (the list shows only active clusters by default) |
| `S` / `R` | Cycle the sort column / reverse the direction |
| `/` | Filter the cluster list |
| `o` | Open the selected cluster in the AWS console |
| `r` | Refresh |
| `i` | About this page · `q` quit |

(In the step history, `y` copies the selected step's failure reason.)

The cluster list shows only **active** clusters by default (`STARTING`,
`BOOTSTRAPPING`, `RUNNING`, `WAITING`, `TERMINATING`), so the often-large
terminated tail neither dominates the view nor costs a `DescribeCluster` each;
press `t` to include it (the status bar shows `active` / `all states`). The CLI
twin honours this too — `emr clusters --all-states`, or naming states with
`--state`, fetches the full set.

### Describe a cluster (`d`)

`d` opens a comprehensive description of the selected cluster, gathered on
demand (loaded once, not on every cursor move), laid out **btop-style** as a
full-screen grid of bordered panels — one per section. `Tab`/`Shift+Tab` (or
`←/→`) move focus between panels; the focused panel scrolls independently
(`↑/↓`, `PgUp/PgDn`, `g/G`) and shows a scrollbar when its content overflows.
On a terminal too short for the grid it collapses to a single scrolling pane so
nothing is clipped. The panels are:

- **Overview** — name, ID, state (and the state-change reason), region, creation
  time, primary-node DNS, ARN.
- **Configuration & OS** — release label, operating system (Amazon Linux, with
  the OS release label and any custom AMI), processor architecture,
  auto-terminate, termination protection, scale-down behaviour, EBS root-volume
  size, log URI, security configuration, service role, instance profile and EC2
  key.
- **S3 connector** — the effective S3 connector (**EMRFS** vs **S3A**) derived
  from the release label (S3A is the default from `emr-7.10.0`) and any explicit
  `core-site fs.s3.impl` override, plus **EMRFS Consistent View** status (and its
  DynamoDB metadata table, flagged obsolete when on) and S3 encryption. Pure read
  of the describe response — no extra API calls. The same derivation backs the
  `EMR-EMRFS-001` audit check.
- **Services** — the installed applications and their versions.
- **Compute, memory & storage** — each instance group (or fleet): node role,
  instance type and market, running/requested counts, per-instance **memory**
  and **vCPU** (resolved from EC2), and the attached **EBS** volumes (type, size,
  IOPS).
- **EC2 instances** — the cluster's running instances (EC2 id, type, state,
  private DNS).
- **Networking** — the cluster's **VPC**, subnet (CIDR, AZ, public-IP-on-launch),
  the **security groups** EMR attached (managed primary/core-task, service
  access, and any additional) with their **inbound/outbound rules**, the
  subnet's effective **route table**, and its **network ACL** entries.
- **Configurations** — the cluster's configuration classifications and their
  properties (`spark-defaults`, `core-site`, …).

Every section is **best-effort**: a denied or throttled API call degrades just
that section and is recorded under a **Notes** block at the bottom, so a gap
reads as "couldn't fetch" rather than "none". Memory/vCPU come from
`ec2:DescribeInstanceTypes`; the networking section uses read-only `ec2:Describe*`
calls in addition to the EMR API.

The **Findings** panel reuses the same deterministic checks as `audit`
(`EMR-COST-*`, `EMR-LOG-001`, `EMR-SEC-001`, `EMR-STEP-002`) over the data
already on screen — no extra AWS calls. (The latest-step check needs per-cluster
step history, loaded lazily, so it stays silent here.)

`L` opens the [S3 browser](s3.md) rooted at the cluster's log archive
(`<LogUri>/<cluster-id>/`), or at a specific step's folder
(`…/steps/<step-id>/`) from the step view. Clusters with no `LogUri` show a
toast instead.

`u` provisions (or reuses) the cluster's **persistent application UIs** —
Spark History Server, YARN Timeline Server or Tez UI — and opens a presigned
link to the chosen one. These are hosted **off-cluster**, so the link needs no
SSH tunnel and stays valid for 30 days after the application terminates.

### On-cluster access (live YARN, HBase & Oozie)

The live **YARN** application browser (`y`, ResourceManager REST API), the
**HBase** table browser (`h`, HBase REST server) and the **Oozie** workflow /
coordinator browser (`z`, Oozie REST API) read daemons that run on the cluster's
primary node. These have **no AWS API** — they're reachable only from inside the
cluster's VPC — so this is **opt-in** and **off by default**. Enable it under
`emr.onCluster` in `config.yaml`:

```yaml
emr:
  onCluster:
    mode: socks            # off (default) | direct | socks | tunnel
    socksProxy: 127.0.0.1:8157
    ssh:                   # used by 'tunnel' mode
      user: hadoop
      keyFile: ~/.ssh/emr.pem
```

- **`direct`** — the tool is already inside the VPC (bastion / in-VPC CloudShell
  / peered network); plain HTTP to the primary node.
- **`socks`** — route through a SOCKS5 proxy, e.g. an SSH dynamic tunnel you
  already run (the pattern AWS documents for the cluster web UIs):

  ```bash
  ssh -i <key.pem> -N -D 8157 hadoop@<primary-dns>
  ```

- **`tunnel`** — let the tool open its **own** SSH connection to the primary node
  (using `ssh.user` / `ssh.keyFile`) and dial the daemon through it — no separate
  `ssh -D` needed. The key must be unencrypted. Because EMR primary nodes are
  ephemeral and not in `known_hosts`, this mode does not pin the host key.

When access is off or the daemon can't be reached, the browser shows a "how to
connect" helper (including the exact tunnel command) instead of an error — every
on-cluster request is a read-only `GET` with a timeout.

#### Debugging a connection — `emr connect-check`

When a live view won't load, `emr connect-check <cluster-id>` verifies the
connection **one layer at a time** and tells you exactly what to fix, instead of
a single opaque "unreachable". It walks the same path a real connection uses:

1. **config** — is `emr.onCluster` configured and does the dialer build?
2. **cluster** — does `DescribeCluster` work, is the cluster running, is the
   primary-node DNS resolved?
3. **bridge** — the link into the VPC: is the SOCKS proxy listening (`socks`),
   can the tool SSH to the primary node (`tunnel`, distinguishing an auth failure
   from a blocked port 22), or is it in-VPC (`direct`)?
4. **service** — for each daemon: is its port reachable through the bridge, and
   does it answer its health endpoint?

A failure short-circuits the layers that depend on it (shown as `SKIP`), so the
report never implies it verified something it couldn't reach. Each line carries a
concrete next step:

```text
EMR connect-check — j-1A2B3C4D5 [us-east-1]

  ✓ OK   on-cluster config          mode=socks · proxy 127.0.0.1:8157
  ✓ OK   cluster                    RUNNING — primary ec2-…compute.amazonaws.com
  ✗ FAIL bridge (socks)             SOCKS proxy 127.0.0.1:8157 not reachable
      → No SSH dynamic tunnel is running. In a separate terminal run:
      → ssh -i <key.pem> -N -D 8157 hadoop@ec2-…  — and confirm
      → emr.onCluster.socksProxy matches that -D port.
  · SKIP HBase                      skipped — bridge not available

Summary: 2 OK · 1 failed · 1 skipped — fix the first ✗ above and re-run.
```

Scope it with `--service hbase,oozie` (default `all` = `hbase,yarn,oozie,hive`).
It exits non-zero when any check fails, so it works as a pre-flight gate in
scripts. **Hive** is a TCP **port-reachability** check only — HiveServer2 speaks
Thrift, not HTTP, so the port can be confirmed open but not protocol-checked
(use `beeline` for a full check); YARN/HBase/Oozie get true REST health checks.

The **HBase browser** lists namespaces → tables with a derived **state**
(`ENABLED` / `DISABLED` / `PARTIAL`, inferred from how many of a table's regions
are assigned), the **region count**, **online regions**, and **column
families** (all exact). HBase has no cheap row count, so the **ROWS** column
shows `—` until you ask for it: press **`c`** on a table to run an exact
**full-table scan** (read-only but not free — it prompts for confirmation first,
is bounded at 5M rows, and shows a `+` when capped). The CLI twin exposes the
same scan as `emr hbase <id> --count ns:table`.

The **Oozie browser** has two tabs (`Tab` switches them): **Workflows** (name ·
status · user · start time) and **Coordinators** (name · status · frequency ·
next-materialized time), colour-coded by status. See
[`emr-design.md`](emr-design.md) for the full design.

> **Scope note.** Everything else in this dashboard covers the EMR **control
> plane** (clusters, steps) and **history plane** (logs, persistent UIs) via the
> AWS API and needs no on-cluster access.

### Scriptable twins

```bash
aws_explorer emr clusters       [--all-regions] [--state RUNNING,WAITING] [-o table|json|ndjson|csv]
aws_explorer emr steps <id>     [-r us-east-1] [--limit 50] [--status FAILED] [-o …]
aws_explorer emr instances <id> [-r us-east-1] [--limit N] [-o …]
aws_explorer emr apps <id>      [-r us-east-1] [-o …]
aws_explorer emr describe <id>  [-r us-east-1] [-o table|json|ndjson]   # full describe (config, OS, compute, storage, networking)
aws_explorer emr yarn <id>      [-r us-east-1] [-o …]   # live YARN apps (on-cluster)
aws_explorer emr hbase <id>     [-r us-east-1] [-o …]   # HBase tables (on-cluster)
aws_explorer emr oozie <id>     [-r us-east-1] [--coordinators] [-o …]   # Oozie jobs (on-cluster)
aws_explorer emr hbase <id>     --count ns:table [-r us-east-1]          # exact row count (full scan)
```

```bash
# Which clusters are still up?
aws_explorer emr clusters --state RUNNING,WAITING -o json | jq '[.[].name]'

# Failed steps of one cluster
aws_explorer emr steps j-1A2B3C4D5 --status FAILED -o json | jq '.[] | {name, failureReason}'
```

The clusters JSON exposes `state`, `releaseLabel`, `applications`,
`normalizedInstanceHours` and `autoTerminate`; the steps JSON exposes
`durationSeconds`, ISO-8601 `started`/`ended` and the `failureLog` S3 pointer.
`steps` is region-specific: it uses `--region` when given, otherwise the first
region in scope.

**IAM permissions.** Read-only:
`elasticmapreduce:{ListClusters,DescribeCluster,ListSteps,ListInstances}`, plus
`elasticmapreduce:{ListInstanceGroups,ListInstanceFleets}` and
`ec2:{DescribeInstanceTypes,DescribeSubnets,DescribeSecurityGroups,DescribeRouteTables,DescribeNetworkAcls}`
for the `d` describe view's compute/storage/memory and networking sections, plus
`elasticmapreduce:{CreatePersistentAppUI,DescribePersistentAppUI,GetPersistentAppUIPresignedURL}`
for the `u` application-UI links, and the [S3 browser](s3.md)'s `s3:*`
read actions for the `L` log jump. The describe view's EC2/instance-group calls
are best-effort — a denial degrades just that section with a note. Per-region or per-cluster denials degrade
that part of the dashboard with a logged note and never abort the session. The
`y` (YARN), `h` (HBase) and `z` (Oozie) browsers use no IAM — they talk to the
on-cluster ResourceManager / HBase REST server / Oozie server — but need
`emr.onCluster` configured and network reachability into the VPC (and the
security group must allow the daemon ports: YARN 8088, HBase REST 8080, Oozie
11000).
