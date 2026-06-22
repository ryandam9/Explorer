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

Flags: `--depth 1-5`, `--direction both|uses|usedby`, `-o table|json|ndjson|csv`,
`--no-header`, plus the global `-r/--region` and `--all-regions`. The coverage
caveat and any per-region failures go to **stderr**, so stdout stays clean for
pipelines.

### Interactive explorer

```bash
aws_explorer related <arn-or-id> --tui
```

Two stacked panels — **Uses (depends on) →** on top, **Used by ←** below — for
the centred resource. `Enter` re-centres on a linked resource and a breadcrumb
tracks the path; `Esc` steps back. `Tab` / `←` / `→` switch panels; `y` copies
the ARN, `o` opens the console; `r` re-scans; `i` opens scrollable help; `q`
quits. Edges are collected once, so moving around never hits AWS.

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

## 8. Related docs

- [Find / whereused / related — command reference](find.md)
- [Enhancement roadmap](enhancement-roadmap.md) (AXE-009 where-used, AXE-010
  graph export — the same edge index can serialise to DOT/Mermaid)
