# The `related` feature — resource relationship discovery

> Given one AWS resource, show everything linked to it — in **both** directions,
> across **all the common services**, optionally **several hops** out, and
> **honestly** (an empty answer is always scoped to what was actually checked).

This document explains what the feature is for, how it works end to end, the
full coverage it provides, and — just as importantly — what it deliberately does
**not** do. For the day-to-day command reference see
[`find.md`](find.md#related-bidirectional); this is the design deep-dive.

---

## 1. Purpose

Operating an AWS account constantly raises *relationship* questions:

- "I'm about to delete this IAM role / KMS key / security group — **what breaks?**"
- "This Lambda is misbehaving — **what is it wired to?** Its role, its VPC, its
  log group, the queue that triggers it?"
- "An object lands in this S3 bucket — **what runs?**"
- "Who can decrypt with this KMS key? What references this subnet?"

AWS exposes the answers, but scattered across dozens of `Describe*` / `Get*`
calls and embedded in fields the normal inventory never keeps (a Lambda's
execution role, a bucket's notification target, an alarm's actions). The
`related` feature collects those *linking fields* once and lets you ask the
relationship question directly — from either end.

It generalises two earlier, narrower tools:

| Tool | Question it answers | Scope |
|------|--------------------|-------|
| `whereused` (AXE-009) | "what **points at** this?" (reverse only) | 4 target kinds |
| **`related`** | "what does this **use**, and what **uses** it?" (both ways, multi-hop) | any resource |

The summary TUI's `x` cross-reference does a blind substring scan of the
collected inventory; `related` instead does **typed reference resolution**.

---

## 2. Mental model: a graph of typed edges

Everything rests on one idea — a directed **edge**:

```
From  ──(via R)──▶  Target
```

> "Resource **From** references identifier **Target** via relationship **R**."

For example:

```
lambda:checkout  ──(execution role)──▶  iam:role/app
s3:media         ──(S3 event notification → Lambda)──▶  lambda:thumbnailer
ec2:i-123        ──(attached security group)──▶  sg-0abc
```

Collection emits a flat list of these edges. Two indexes are built over that
list:

- **Reverse index** — keyed by `Target` → the resources that reference it.
  Answers **"used by ←"** (this is exactly what `whereused` uses).
- **Forward index** — keyed by `From` → the edges originating there.
  Answers **"uses (depends on) →"**.

Because each edge stores *both* ends, the same collected data answers both
directions for free. A query resolves an input identifier against both indexes
and, for multi-hop, keeps walking.

The core types (`internal/xref/xref.go`, `related.go`):

```go
type Edge struct {
    From   Reference // the source resource (service, type, id, name, region, via)
    Target string    // the referenced identifier (ARN or short id)
}

type Reference struct {
    Service, Type, ID, Name, Region, Via string
}

type Link struct {       // one related resource, as returned to the caller
    Reference
    Depth int            // 1 = directly related, 2 = one hop further, …
    Path  string         // chain of relationship labels, e.g. "execution role ▸ attached managed policy"
}

type RelatedResult struct {
    Target       Target
    Depth        int
    Uses         []Link   // forward
    UsedBy       []Link   // reverse
    CheckedTypes []string // reference types checked for the target's kind (scoping)
}
```

---

## 3. How it works, end to end

### 3.1 Collection (`internal/xref/collect*.go`)

A single account sweep produces every edge. Work is split by *where the service
lives*:

- **Per-region fan-out** (`collectRegion`, bounded concurrency) for regional
  services: Lambda, EC2, ECS, EKS, RDS, DynamoDB, ElastiCache, Redshift, SQS,
  SNS, EventBridge, Step Functions, Kinesis, ELBv2, API Gateway, VPC endpoints,
  EFS, Secrets Manager, KMS, CloudWatch (alarms + logs).
- **Global, once** for services that aren't per-region:
  - **IAM** — instance-profile→role, trust principals, role→policies — resolved
    up front so EC2 can attribute instance profiles to roles.
  - **S3** — listed globally, but each `GetBucket*` is issued against the
    **bucket's own region** (see §3.4).
  - **CloudFront** and **Route 53** — global services, collected against
    `us-east-1`.

Each extractor is split into a **pure mapping function** over the SDK's response
struct (e.g. `lambdaFunctionEdges`, `s3BucketEdges`, `rdsInstanceEdges`) and a
thin **wrapper** that paginates the AWS call and delegates. The pure functions
are deterministic and fixture-tested with fake clients — no AWS needed to verify
the mapping, pagination, and partial-failure behaviour.

### 3.2 Indexing & querying (`internal/xref/related.go`)

```go
fwd := xref.BuildForwardIndex(edges)  // From → edges
rev := xref.BuildIndex(edges)         // Target → references
res := xref.Related(input, fwd, rev, depth)
```

`Related` classifies the input into its identifier set (ARN **and** short form,
so a query by name resolves edges stored by ARN and vice-versa), then runs two
breadth-first walks:

- **`walkForward`** — hop 1 is what the input references, hop 2 is what *those*
  reference, etc.
- **`walkReverse`** — hop 1 is what references the input, hop 2 is what
  references *those*.

Both walks:

- **deduplicate** rows by resource + relationship (shortest path wins);
- **guard against cycles** with a visited set over both identifier forms (a
  role that trusts a role that trusts the first won't loop);
- record a **`Path`** — the chain of `via` labels — and a **`Depth`** per row;
- are bounded by `--depth` (default 1, max 5).

Navigation in the TUI uses depth 1 and treats each "open" as one hop (a
re-center), which reads more naturally than a flattened multi-hop dump.

### 3.3 The honesty contract (the most important part)

A relationship tool is dangerous if "nothing found" is read as "nothing exists".
`related` never does that:

- Every result carries **`CheckedTypes`** — the human-readable list of reference
  types collection actually scans for the target's kind (for the recognised
  kinds: IAM role, KMS key, ACM cert, security group). An empty "Used by" for an
  IAM role prints *"Not referenced by anything checked: Lambda execution roles,
  EC2 instance profiles, …"* — bounded, not absolute.
- An **always-printed caveat** states that only collected relationship types are
  shown.
- Collection is **best-effort**: a denied or failed API call is **recorded**
  (surfaced on stderr / flagged in the TUI), never swallowed into a false
  "none". A region that fails narrows what was checked and says so.
- Facts that can't be observed cleanly are **labelled as derived** — e.g. a
  Lambda's log group (`/aws/lambda/<name>` by convention) and Route 53 alias
  targets (matched by DNS name, not ARN) carry "(by convention)" /
  "(DNS-derived)" in their relationship label.

This mirrors the repository-wide rules in `CLAUDE.md` (§6a no swallowed errors,
§8 tri-state on unknowns).

### 3.4 Region & identifier correctness

- Every edge's `From` carries the **source resource's region**.
- **Globally-listed ≠ globally-callable.** S3 buckets list in one call, but the
  per-bucket configuration calls must hit the bucket's *own* region, so S3 runs
  a per-bucket region client (the lesson from #323).
- Identifiers are matched on **both an ARN and its short form**, so
  `related app`, `related arn:aws:iam::…:role/app`, and an edge stored under
  either all resolve to the same node.
- A handful of identifiers are **classified from their shape** for nicer display
  — `sg-…`, `subnet-…`, `ami-…`, `eipalloc-…`, and `/aws/…` or `/ecs/…` log
  group names.

### 3.5 Cost & performance

- The query itself is **in-memory** — once edges are collected, walking the
  graph (and, in the TUI, every navigation hop) makes **no further AWS calls**
  (`CLAUDE.md` §7).
- Collection cost scales with account size. It uses paginators throughout and
  bounded per-region concurrency. A few extractors are unavoidably N+1 (e.g.
  `DescribeTargetHealth` per target group, `GetKeyPolicy` per KMS key); these
  are best-effort and collapse failures rather than aborting.
- All APIs used are **read-only and free** (no Cost Explorer, no paid calls).

---

## 4. Coverage

What links are understood today. Each row is one or more edge types; the feature
is designed so new extractors plug into the same index without touching the query
or UI.

| Service area | Edges emitted (`From → Target`) |
|---|---|
| **Lambda** | execution role, env KMS key, VPC subnets & security groups, layers, dead-letter target, CloudWatch log group *(derived)*, event-source mappings (SQS/DynamoDB/Kinesis/MSK) |
| **EC2** | instance-profile → role, subnet, AMI, key pair, attached ENIs, Elastic IPs; EBS volume → KMS & attached instance; ENI → security groups |
| **ECS** | task & execution roles, container log groups, container Secrets Manager/SSM references |
| **EKS** | cluster & node-group roles, control-plane & additional security groups, subnets, OIDC provider |
| **S3** | event notifications → Lambda/SNS/SQS, replication role & destination bucket, access-log target bucket, default SSE-KMS key |
| **EFS** | KMS key, mount-target subnets & security groups |
| **RDS / Aurora** | KMS, security groups, subnet group & subnets, parameter & option groups, master-user secret, enhanced-monitoring role; **cluster** ↔ member instances, associated roles |
| **DynamoDB** | encryption key, stream |
| **ElastiCache** | security groups, subnet group; replication group → KMS & member clusters |
| **Redshift** | cluster IAM roles, KMS, security groups, subnet group |
| **SNS** | subscriptions (SQS/Lambda/HTTP(S)/email/Firehose), topic KMS |
| **SQS** | dead-letter queue (redrive), queue KMS |
| **EventBridge** | rule → targets (Lambda/SNS/SQS/Step Functions/Kinesis/ECS/…), event bus, dead-letter |
| **Step Functions** | execution role, ARNs referenced in the ASL definition |
| **Kinesis** | stream KMS, registered consumers |
| **ELBv2** | listener → ACM cert, LB → security groups & subnets, target group → load balancer & registered targets (instances/IPs/Lambda) |
| **API Gateway** | (HTTP/WebSocket) Lambda integrations & authorizers, VPC links → subnets/SGs; (REST) Lambda authorizers |
| **CloudFront** | origins *(DNS-derived)*, viewer ACM cert, WAF web ACL, origin access control |
| **Route 53** | alias record → target *(DNS-derived)* |
| **VPC endpoints** | endpoint service, subnets, security groups |
| **IAM** | role → attached managed & inline policies, trust principals, instance-profile → role |
| **KMS** | key policy principals, grants, aliases → key |
| **Secrets Manager** | encryption key, rotation Lambda |
| **CloudWatch** | alarm → actions (SNS/Lambda/ASG/EC2); log group → subscription filters & KMS |

A nice emergent property: because the derived Lambda/ECS log-group edge and the
CloudWatch Logs subscription edge are both keyed by the **log-group name**,
querying a log group resolves *both* the function that owns it and the
subscription that drains it.

---

## 5. Using it

### CLI

```bash
# Both directions for a Lambda
aws_explorer related arn:aws:lambda:us-east-1:123456789012:function:checkout

# Only what a security group is attached to, in one region
aws_explorer related sg-0abc123 --direction usedby -r eu-west-1

# Walk two hops: a Lambda → its role → that role's policies/trust principals
aws_explorer related arn:aws:iam::123456789012:role/app --depth 2 --all-regions

# Machine-readable (CSV is formula-injection-sanitised)
aws_explorer related sg-0abc123 -o json | jq '.uses'
```

Flags: `--depth 1-5`, `--direction both|uses|usedby`, `--show-paths shortest|all`,
`-o table|json|ndjson|csv`, `--no-header`, `--cache-ttl <dur>`, `--refresh`,
`--debug-scan`, plus the global `-r/--region` and `--all-regions`. The coverage
caveat and any per-region failures go to **stderr**, so stdout stays clean for
pipelines. The report is printed **first**; the collection-error summary follows
**after** it (so results aren't buried under the errors). Status and error lines
on a terminal carry a colored `INFO`/`WARNING`/`ERROR` level tag — matching the
CLI's own leveled logs — and the resource you queried is highlighted where it's
echoed back (disabled by [`NO_COLOR`](https://no-color.org/) or when piped).

- `--show-paths all` keeps every distinct path to a resource (default keeps the
  shortest); the table then shows the full `PATH` chain instead of `VIA`.
- `--no-header` emits data rows only, each prefixed with its direction
  (`uses`/`used_by`) in place of the cosmetic `SNO` column — for `awk`/`cut`.
  For fully-structured output prefer `-o csv`/`-o ndjson`.
- `--cache-ttl 5m` reuses a recent scan of the same scope; `--refresh` forces a
  live rescan. `--debug-scan` prints per-service scan timings to stderr.
- `-o json` carries `"partial": true` and an `"errors"` array when collection
  hit failures, so automation can tell "no relationships" from "scan incomplete"
  without parsing stderr.
- A bare name that matches several resources (e.g. `app` → two role ARNs) prints
  an ambiguity warning listing the candidates; pass a full ARN to disambiguate.
- `--depth`/`--direction` are rejected in `--tui` mode (the explorer walks one
  hop per Enter and shows both directions) rather than silently ignored.

### Interactive explorer

```bash
aws_explorer related <arn-or-id> --tui
```

Two stacked panels — **Uses (depends on) →** on top, **Used by ←** below — for
the centred resource. `Enter` re-centres on a linked resource and a breadcrumb
tracks the path; `Esc` steps back. `Tab` / `←` / `→` switch panels; `y` copies
the ARN, `o` opens the console; `r` re-scans; `i` opens scrollable help; `q`
quits. Edges are collected once, so moving around never hits AWS.

### Jump in from the summary TUI

You don't have to start from the command line: in the summary TUI, select a
resource (open its detail panel — `Ctrl+P` fuzzy-find gets you there fast) and
press **`R`** to open the related explorer centred on it. The summary TUI
suspends, the explorer runs in the same terminal, and quitting it returns you
exactly where you were. (Each jump re-runs the account scan, so there's a brief
"scanning…" spinner.)

### Examples by resource type

Pass a full ARN, a bare resource id, or (for the named services) a bare name.
For regional resources add `-r <region>` (or `--all-regions`).

#### Query the resource itself — what it depends on (`--direction uses`, the default includes this)

| Resource type | Example invocation | Reveals (the links it follows) |
|---|---|---|
| Lambda function | `related arn:aws:lambda:us-east-1:111122223333:function:checkout -r us-east-1` | execution role, env KMS key, VPC subnets, VPC SGs, layers, dead-letter target, log group, event source |
| EC2 instance | `related i-0abc123def -r us-east-1` | instance-profile role, subnet, AMI, key pair, attached ENIs, Elastic IP |
| EC2 network interface | `related eni-0abc123 -r us-east-1` | attached security groups, Elastic IP |
| EBS volume | `related vol-0abc123 -r us-east-1` | encryption key, attached-to instance |
| ECS task definition | `related arn:aws:ecs:us-east-1:111122223333:task-definition/web:7 -r us-east-1` | task role, execution role, container log groups, container secrets |
| EKS cluster | `related arn:aws:eks:us-east-1:111122223333:cluster/prod -r us-east-1` (or `related prod`) | cluster role, cluster SG, extra SGs, subnets, OIDC provider |
| EKS node group | `related arn:aws:eks:us-east-1:111122223333:nodegroup/prod/ng-1/abc -r us-east-1` | node-group role |
| RDS DB instance | `related orders -r us-east-1` (or its ARN) | storage KMS key, DB SGs, subnet group, subnets, parameter/option groups, master-user secret, monitoring role, parent cluster |
| RDS/Aurora cluster | `related orders-cluster -r us-east-1` | KMS key, DB SGs, subnet group, master-user secret, associated roles, member instances |
| DynamoDB table | `related Orders -r us-east-1` (or ARN) | encryption key, stream |
| ElastiCache cache cluster | `related my-redis-001 -r us-east-1` | security groups, cache subnet group |
| ElastiCache replication group | `related my-redis -r us-east-1` | encryption key, member cache clusters |
| Redshift cluster | `related my-warehouse -r us-east-1` | IAM roles, encryption key, security groups, subnet group |
| ELBv2 load balancer | `related arn:aws:elasticloadbalancing:us-east-1:111122223333:loadbalancer/app/web/abc -r us-east-1` | load balancer security groups, subnets |
| ELBv2 target group | `related arn:aws:elasticloadbalancing:us-east-1:111122223333:targetgroup/web/abc -r us-east-1` | attached load balancers, registered targets (instances/IPs/Lambda) |
| ELBv2 listener | `related arn:aws:elasticloadbalancing:us-east-1:111122223333:listener/app/web/abc/def -r us-east-1` | listener certificate |
| API Gateway (HTTP/REST) | `related a1b2c3d4 -r us-east-1` (the API id) | Lambda integrations, Lambda authorizers |
| API Gateway VPC link | `related <vpc-link-id> -r us-east-1` | subnets, security groups |
| VPC endpoint | `related vpce-0abc123 -r us-east-1` | endpoint service, subnets, security groups |
| CloudFront distribution | `related arn:aws:cloudfront::111122223333:distribution/E123ABC` (global) | origins (by DNS), origin access control, viewer ACM cert, WAF web ACL |
| Route 53 alias record | `related www.example.com.` | alias target (by DNS name) |
| SNS topic | `related orders -r us-east-1` (or ARN) | subscriptions (SQS/Lambda/HTTP/email…), topic encryption key |
| SQS queue | `related orders -r us-east-1` (or ARN) | queue encryption key, dead-letter queue |
| EventBridge rule | `related my-rule -r us-east-1` (or ARN) | targets, target dead-letter queues, event bus |
| Step Functions state machine | `related my-sm -r us-east-1` (or ARN) | execution role, ARNs referenced in its definition |
| Kinesis stream | `related events -r us-east-1` (or ARN) | encryption key, registered consumers |
| S3 bucket | `related my-bucket` (or `arn:aws:s3:::my-bucket`) | event notifications → Lambda/SNS/SQS, replication role + destination, access-log target bucket, default SSE-KMS key |
| EFS file system | `related fs-0abc123 -r us-east-1` (or ARN) | encryption key, mount-target subnets, mount-target security groups |
| IAM role | `related app` (or `arn:aws:iam::111122223333:role/app`) | trust-policy principals, attached managed policies, inline policies |
| KMS key | `related arn:aws:kms:us-east-1:111122223333:key/<id> -r us-east-1` | key-policy principals, grant grantees |
| KMS alias | `related alias/my-key -r us-east-1` | target key |
| CloudWatch alarm | `related HighCPU -r us-east-1` (or ARN) | alarm/OK/insufficient-data actions |
| CloudWatch Logs log group | `related /aws/lambda/checkout -r us-east-1` | encryption key, subscription-filter destinations |
| Secrets Manager secret | `related db-creds -r us-east-1` (or ARN) | encryption key, rotation Lambda |

#### Query a shared dependency — what uses it (`--direction usedby`)

The four classified kinds also print a scoped "checked these link types" footer.

| Query | Example | Reveals |
|---|---|---|
| IAM role | `related arn:aws:iam::111122223333:role/app --direction usedby` | Lambda/EC2/ECS/EKS/Redshift/RDS/Step Functions that assume it, plus trust principals |
| KMS key | `related arn:aws:kms:us-east-1:111122223333:key/<id> --direction usedby -r us-east-1` | every resource encrypted with it (EBS, RDS, S3, Secrets, SQS, SNS, DynamoDB, EFS, Kinesis, Logs, …) |
| ACM certificate | `related arn:aws:acm:us-east-1:111122223333:certificate/<id> --direction usedby -r us-east-1` | ELBv2 listeners, CloudFront distributions |
| Security group | `related sg-0abc123 --direction usedby -r us-east-1` | ENIs, Lambda VPC config, EKS, load balancers, RDS, ElastiCache, Redshift, VPC endpoints, EFS mount targets |
| Subnet / AMI / log group / … | `related subnet-0abc123 --direction usedby -r us-east-1` | anything placed in / referencing it (no scoped footer — not a classified kind) |

> **Note on coverage.** A security group has *no* "depends on" edges (it only ever appears as the target of an attachment), so for SGs the meaningful direction is `--direction usedby`. Querying a VPC/route-table/NACL id finds nothing — `related` is a resource-to-resource reference graph, not a VPC inventory (use `aws_explorer vpc`).

---

## 6. Limitations & non-goals

Being explicit here is part of the honesty contract.

1. **Coverage is finite and additive.** Only the relationship types listed in §4
   are understood. A link AWS supports but no extractor reads yet simply won't
   appear — which is *why* every empty result is scoped to `CheckedTypes` and
   carries the caveat. New services/edges are added as small, isolated
   extractors.
2. **Tag-/convention-based and DNS-based links are heuristic.** The Lambda→log
   group edge is inferred from the `/aws/lambda/<name>` naming convention;
   Route 53 alias and CloudFront origin links are matched by **DNS name**, not
   resolved to an ARN. They're labelled as such and may occasionally over- or
   under-match.
3. **A snapshot, not a live graph.** Results reflect the moment of the scan;
   there's no caching or change-tracking. Re-run (or press `r`) to refresh.
4. **Scoped to the configured region(s) and one account.** `--all-regions`
   sweeps every enabled region, but cross-account references (e.g. a role
   assumed from another account, an S3 replication destination you don't own)
   are shown as the raw ARN/name and not resolved.
5. **Not a permission/blast-radius *evaluator*.** `related` shows *structural*
   links (X references Y); it does **not** evaluate IAM policy logic, SCPs,
   resource policies, or "could principal P actually perform action A". For that
   use `iam can` / the IAM simulator.
6. **Deliberately deferred edges** (noted in their PRs, easy follow-ups):
   - Lambda on-success/on-failure **destinations** (per-function call, N+1);
   - API Gateway **REST per-method** backend integrations (resource×method
     walk); REST **authorizers** *are* covered;
   - Redshift **Serverless** (only provisioned clusters today);
   - DynamoDB **GSIs** as intra-table edges.
7. **Cost scales with account size.** Collection is a full read-only sweep; very
   large accounts take longer, and a few extractors are N+1. Bounded concurrency
   and best-effort error collapsing keep it survivable, but it isn't instant.
8. **Multi-hop amplifies.** `--depth` walks transitively; depth is capped at 5
   and cycle-guarded, but deep walks on dense graphs return large result sets.
9. **Read-only by design.** It never mutates AWS and never leaves the AWS API.

---

## 7. Where the code lives

| Path | Responsibility |
|---|---|
| `internal/xref/xref.go` | `Edge`/`Reference`/`Target` types, classification, reverse index, `WhereUsed`, `CheckedTypes` |
| `internal/xref/related.go` | forward index, bidirectional/multi-hop `Related`, walks, link rows |
| `internal/xref/collect.go` | collection orchestration (per-region fan-out + global passes), IAM, SQS, Secrets |
| `internal/xref/collect_*.go` | per-area extractors: `compute`, `storage`, `messaging`, `networking`, `security`, `database`, `observability` |
| `internal/xref/related_render.go` | CLI rendering (table/json/ndjson/csv) |
| `internal/relatedtui/` | the interactive explorer (model + view) |
| `cmd/related.go` | the `related` command (CLI + `--tui`) |

History: the feature was built incrementally under the umbrella issue **#336**
("Related resources: bidirectional relationship discovery") — a core engine
(#337) plus per-area edge extractors (#338–#344) and the TUI (#345). It builds
on and generalises the shipped `whereused` / `internal/xref` work (AXE-009).

## 8. Troubleshooting & interpretation

**Safe interpretation of an empty result.** An empty side never means "this
resource is isolated". Read it as: *no relationships were found among the link
types that were scanned successfully*. Untyped link types, unsupported services,
denied APIs, and timed-out regions all narrow what was checked — and a denied or
failed call is reported on stderr (and, for `-o json`, as `partial`/`errors`),
not silently folded into "none". Before treating "Used by → (none found)" as
"safe to delete", confirm the scan wasn't partial.

Common questions:

- **Why no "Used by" results?** Either the resource genuinely isn't referenced
  by a collected link type, or the relevant collector failed (check stderr / the
  JSON `errors`). For a security group, "Uses" is *always* empty by design — SGs
  only appear as the target of attachments, never as a source.
- **Why is a row missing its service/type/region?** Short-ID targets are
  classified best-effort and inherit the source region; truly unrecognized names
  (e.g. a DB subnet-group) are shown without a service/region rather than
  guessed.
- **Why did `related vpc-…` find nothing?** `related` is a resource-to-resource
  reference graph, not a container inventory. To list what lives inside a VPC,
  use `aws_explorer vpc`.
- **Why does `--tui` rescan after launching from the summary TUI?** Each process
  collects independently. Use `--cache-ttl 5m` to reuse a recent scan.
- **Scan is slow / floods deadline errors.** Scope with `-r REGION`, raise the
  app timeout, and use `--debug-scan` to see which service dominates. The IAM
  role-policy sweep only runs when the query can land on a role (a role target
  or `--depth > 1`).

## 9. Performance expectations

The cost is **collection**, not the in-memory graph walk. Rough guidance:

| Account size | Typical scan |
|---|---|
| Small (one region, few resources) | seconds |
| Medium | tens of seconds |
| Large, multi-region (`--all-regions`) | minutes possible |

Per-item sweeps (KMS, DynamoDB, SNS, Step Functions, ECS) are bounded-concurrent;
the per-region fan-out is bounded too. Use `--debug-scan` for a per-service
breakdown and `--cache-ttl` to avoid rescanning during repeated exploration.

## 10. Recipes

```bash
# Before deleting a security group — what's attached, across all regions?
aws_explorer related sg-0abc123 --direction usedby --all-regions

# Before disabling a KMS key — everything it encrypts, two hops out
aws_explorer related arn:aws:kms:us-east-1:123:key/abc --depth 2 --all-regions

# Debug an S3 object-created pipeline
aws_explorer related arn:aws:s3:::my-bucket --depth 2

# Debug a Lambda's event sources and dependencies
aws_explorer related arn:aws:lambda:us-east-1:123:function:checkout --depth 2

# Scriptable: all of a role's consumers, one row per line
aws_explorer related arn:aws:iam::123:role/app --direction usedby --no-header

# Repeated exploration without rescanning each time
aws_explorer related arn:aws:iam::123:role/app --cache-ttl 5m
```

> Module vs repository name: the Go module path is `github.com/ryandam9/aws_explorer`
> (the binary is `aws_explorer`); the GitHub repository is `ryandam9/Explorer`.

## 11. Related docs

- [Find / whereused / related — command reference](find.md)
- [Enhancement roadmap](enhancement-roadmap.md) (AXE-009 where-used, AXE-010
  graph export — the same edge index can serialise to DOT/Mermaid)
