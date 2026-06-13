# AWS Explorer — Enhancement Roadmap & Design Specification

Status: **In progress** — shipped so far: AXE-004 & the `internal/findings` platform (#79), AXE-023 (#80), AXE-001 & AXE-022 (#81), AXE-013 (#82), AXE-007 (#83), AXE-005 (#84, #86), AXE-006 (#85), AXE-012 (#87), AXE-002 (#88), AXE-008 (#89), AXE-003 (#90), AXE-018 · Tracking issue: #76

This document specifies 24 proposed enhancements, grouped into nine themes.
Each enhancement carries a stable ID (`AXE-NNN`) used in the tracking issue,
commit messages, and PR titles, so any piece of work can be referenced
unambiguously (e.g. `AXE-004: cost linter — flag unassociated EIPs`).

The proposals follow the tool's established design principles:

1. **Deterministic, no AI** — every analysis is a pure function over data AWS
   returns, unit-testable with fixture snapshots (the pattern set by
   `internal/vpctui/findings.go`).
2. **Read-only by default** — anything that mutates AWS or incurs a charge is
   opt-in with an explicit cost-stating confirmation (the pattern set by
   Reachability Analyzer in `internal/vpctui/analyzer.go`).
3. **Best-effort collection** — a denied API call degrades a feature, never
   crashes it; partial results are kept and flagged.
4. **One UX language** — findings render in the severity/resource/issue/fix
   table style; every new table uses the shared theme/table machinery in
   `internal/ui` and `internal/table`.

---

## Contents

| ID | Title | Theme | Priority |
|----|-------|-------|----------|
| [AXE-001](#axe-001) | Decode encoded authorization failure messages | A — IAM / access debugging | ✅ shipped (#81) |
| [AXE-002](#axe-002) | IAM policy simulator ("can X do Y on Z?") | A — IAM / access debugging | ✅ shipped |
| [AXE-003](#axe-003) | IAM hygiene linter | A — IAM / access debugging | ✅ shipped |
| [AXE-004](#axe-004) | Cost/waste linter with monthly estimates | B — Cost & waste | ✅ shipped (#79) |
| [AXE-005](#axe-005) | CloudTrail "who changed this" | C — Change attribution & drift | ✅ shipped |
| [AXE-006](#axe-006) | Account-wide inventory snapshot diff | C — Change attribution & drift | ✅ shipped |
| [AXE-007](#axe-007) | Expiry & deprecation watchlist (`expiring`) | D — Expiry & deprecation | ✅ shipped |
| [AXE-008](#axe-008) | Account-wide security audit (`audit`) | E — Account-wide audit | ✅ shipped |
| [AXE-009](#axe-009) | Generalized where-used / blast radius | E — Account-wide audit | P2 |
| [AXE-010](#axe-010) | Relationship graph export (DOT / Mermaid) | E — Account-wide audit | P3 |
| [AXE-011](#axe-011) | Jump from resource to its CloudWatch logs | F — Cross-navigation | P2 |
| [AXE-012](#axe-012) | Open selected resource in the AWS console | F — Cross-navigation | ✅ shipped |
| [AXE-013](#axe-013) | Global fuzzy finder | F — Cross-navigation | ✅ shipped (#82) |
| [AXE-014](#axe-014) | Inline CloudWatch metric sparklines | F — Cross-navigation | P3 |
| [AXE-015](#axe-015) | ECS stopped-task triage | G — Service-specific triage | P2 |
| [AXE-016](#axe-016) | Lambda triage view | G — Service-specific triage | P2 |
| [AXE-017](#axe-017) | Service-quota dashboard | G — Service-specific triage | P2 |
| [AXE-018](#axe-018) | SQS/SNS plumbing checks | G — Service-specific triage | ✅ shipped |
| [AXE-019](#axe-019) | Path tracer: IPv6 evaluation | H — Tracer completeness | P2 |
| [AXE-020](#axe-020) | Path tracer: managed prefix-list expansion | H — Tracer completeness | P2 |
| [AXE-021](#axe-021) | Multi-account scanning | I — Multi-account & automation | P2 |
| [AXE-022](#axe-022) | SSO-aware auth errors | I — Multi-account & automation | ✅ shipped (#81) |
| [AXE-023](#axe-023) | CI mode: exit codes, `--fail-on`, SARIF | I — Multi-account & automation | ✅ shipped (#80) |
| [AXE-024](#axe-024) | Inventory caching / instant TUI start | I — Multi-account & automation | P3 |

Priorities: **P1** = build first (high demand, modest effort), **P2** = next,
**P3** = valuable but deferrable. A suggested phasing is at the
[end of this document](#phasing).

---

## Architecture context

Where new code plugs in:

- **Collectors** (`internal/services/<svc>/collector.go`) implement the
  `Collector` interface (`internal/services/service.go`) and register in
  `internal/services/registry.go`. They stream page-sized batches via
  `CollectInput.Emit` / `EmitOrAppend`.
- **Engine** (`internal/engine`) fans collectors out across regions with a
  bounded pool, keeps partial results, and aggregates errors.
- **Findings** pattern (`internal/vpctui/findings.go`): a `Finding{Severity,
  Resource, Title, Detail, Fix}` produced by pure check functions over a
  snapshot struct. New linters (AXE-003, AXE-004, AXE-008, AXE-018) should
  extract this into a shared `internal/findings` package rather than
  duplicating it.
- **TUIs**: summary TUI (`internal/tui`), VPC Explorer (`internal/vpctui`),
  S3 browser (`internal/s3tui`), CloudWatch logs (`internal/cwtui`). All are
  Bubble Tea models sharing `internal/ui` (themes, key hints, overlays,
  tables, window titles).
- **Output** (`internal/output`, `internal/csvexport`): table / JSON / NDJSON
  / CSV writers used by CLI commands.
- **Auth** (`internal/auth`): builds `aws.Config` for the five auth methods.

### New shared packages this roadmap introduces

| Package | Introduced by | Purpose |
|---------|---------------|---------|
| `internal/findings` | AXE-003/004/008 | Shared `Finding`, `Severity`, sorting, rendering, JSON/SARIF serialization |
| `internal/costs` | AXE-004 | Static price table + monthly-cost estimation helpers |
| `internal/consolelink` | AXE-012 | ARN → AWS console URL mapping |
| `internal/trail` | AXE-005 | CloudTrail LookupEvents wrapper + per-resource event view |
| `internal/cache` | AXE-024 | On-disk inventory cache under `~/.aws_explorer/cache/` |

---

## Theme A — IAM / access debugging

"Access Denied" is the most common AWS support ticket. The tool should own
this the way it owns "can't connect" with the VPC path tracer.

### AXE-001 — Decode encoded authorization failure messages {#axe-001}

> **Status: ✅ shipped** in #81 as `aws_explorer iam decode` (`cmd/iam.go`, `internal/authzmsg`).

**Problem.** Services like EC2 return opaque
`Encoded authorization failure message: <blob>` errors. Engineers paste these
into ad-hoc scripts to run `sts:DecodeAuthorizationMessage`.

**UX.**

```bash
aws_explorer iam decode <encoded-message>
# or read from stdin:
pbpaste | aws_explorer iam decode -
```

Output: the decoded JSON, plus a human summary extracted from it — the denied
action, the resource ARN, the principal, and whether the denial was an
explicit deny or a missing allow (the decoded document's `allowed`,
`explicitDeny`, `matchedStatements` fields). `-o json` emits the raw decoded
document.

**Implementation.**
- New `cmd/iam.go` with an `iam` parent command; `decode` subcommand.
- One STS call: `DecodeAuthorizationMessage`. Requires
  `sts:DecodeAuthorizationMessage` on the caller.
- Summary extraction is a pure function over the decoded JSON → unit-testable
  with canned documents.

**Acceptance criteria.**
- Decodes a valid blob from arg or stdin; prints summary + full document.
- A denial of the decode permission itself produces a one-line actionable
  error (which IAM permission to add), not an SDK stack trace.

### AXE-002 — IAM policy simulator {#axe-002}

> **Status: ✅ shipped** — `aws_explorer iam can <principal-arn> <action[,action…]> [resource-arn]` (`cmd/iamcan.go`, `internal/iamsim`). The TUI simulate-from-detail-panel hook remains a possible follow-up.

**Problem.** "Why can't role X read bucket Y?" The console policy simulator
is buried and clunky.

**UX.**

```bash
aws_explorer iam can arn:aws:iam::123456789012:role/app s3:GetObject arn:aws:s3:::my-bucket/key
```

Renders a step-by-step verdict in the path-tracer style
(`internal/vpctui/pathtrace.go` is the visual reference):

```
❌ Denied: s3:GetObject on arn:aws:s3:::my-bucket/key for role/app

✓ Identity policies      app-s3-read allows s3:GetObject on arn:aws:s3:::my-bucket/*
✗ Permissions boundary   boundary-policy does not include s3:GetObject
• SCPs / resource policy not evaluated by SimulatePrincipalPolicy — verdict may differ (noted)
```

**Implementation.**
- `iam can <principal-arn> <action> [resource-arn]` using
  `iam:SimulatePrincipalPolicy` (handles identity policies, boundaries, and
  org SCP effects are *not* included — state this caveat explicitly, as the
  path tracer does for prefix lists).
- Map `EvalDecisionDetails` / `MatchedStatements` into the hop list.
- In the TUIs: on an EC2 instance / Lambda detail panel, offer a key to
  simulate against the resource's *instance profile / execution role* with a
  prompted action — reusing the same renderer.

**IAM needed.** `iam:SimulatePrincipalPolicy`, `iam:GetRole` (to resolve the
principal).

**Acceptance criteria.**
- Allowed, implicit-deny, and explicit-deny cases each render distinctly with
  the matched statement(s) named.
- Caveats (SCPs, resource policies, condition keys not supplied) are always
  printed.

### AXE-003 — IAM hygiene linter {#axe-003}

> **Status: ✅ shipped** — `iam` category in `aws_explorer audit` (8 `IAM-*` checks in `internal/findings/iam.go`; credential-report parsing is a pure fixture-tested function; collection in `internal/audit/iam_collect.go`, account-global, run once per audit).

**Problem.** Stale credentials and over-broad policies accumulate silently.

**Checks** (each a pure function; severity in parentheses):

| Check | Source API | Severity |
|-------|-----------|----------|
| Access key older than 90 days / unused 90+ days | `GenerateCredentialReport` + `GetCredentialReport` | 🟡 / 🔴 if active+unused |
| Console user without MFA | credential report | 🔴 |
| Root account access keys exist | credential report | 🔴 |
| Role unused > 90 days (`RoleLastUsed`) | `ListRoles` / `GetRole` | 🔵 |
| Customer policy grants `*:*` or `Action:*` on `Resource:*` | `ListPolicies` + `GetPolicyVersion` | 🔴 |
| Role trust policy allows `"AWS": "*"` | `ListRoles` (inline doc) | 🔴 |
| Policy attached to users directly (vs groups/roles) | `ListEntitiesForPolicy` | 🔵 |

**UX.** `aws_explorer audit --only iam` (part of AXE-008's `audit` command) or
standalone `aws_explorer iam lint`. Renders the standard findings table.

**Implementation.** Extends the existing IAM collector
(`internal/services/iam/collector.go`) with a `DetailLevelDetailed` pass that
fetches the credential report and policy documents; checks live in
`internal/findings/iam.go`. Credential report generation is asynchronous —
poll `GenerateCredentialReport` until `COMPLETE` with a short timeout, degrade
gracefully if denied.

---

## Theme B — Cost & waste

### AXE-004 — Cost/waste linter with monthly estimates {#axe-004}

> **Status: ✅ shipped** in #79 as `aws_explorer audit` (CLI + `--tui`), with `internal/findings`, `internal/costs`, `internal/audit`, `internal/audittui`.

**Problem.** "Why is my bill high?" The most common findable causes are
deterministic and read-only detectable. The existing idle-NAT-gateway check
(`internal/vpctui/findings.go:checkNatGateways`) proves the pattern; this
generalizes it account-wide and attaches a monthly cost estimate.

**Checks.**

| Waste | Detection | Est. basis |
|-------|-----------|------------|
| Unattached EBS volume | `DescribeVolumes` status `available` | size × gp3/gp2/io1 $/GB-mo |
| EBS volume on gp2 (gp3 cheaper, faster) | `DescribeVolumes` type `gp2` | ~20% of gp2 cost |
| Unassociated Elastic IP | `DescribeAddresses` without `AssociationId` | ~$3.6/mo |
| Idle NAT gateway (no route references) | existing check, run account-wide | ~$32/mo + data |
| Load balancer with zero healthy targets | `DescribeTargetHealth` across target groups | ALB ~$16/mo, NLB ~$16/mo |
| Stopped instance with attached EBS | `DescribeInstances` state `stopped` | sum of volume costs |
| Snapshot older than N days (default 180) with no AMI reference | `DescribeSnapshots` (self-owned) + `DescribeImages` | size × $0.05/GB-mo |
| Unused AMI (no instance launched from it, older than N days) | `DescribeImages` + instance image-ids | backing snapshot cost |
| DynamoDB provisioned table with consumption ≪ provision | `DescribeTable` + CloudWatch `ConsumedReadCapacityUnits` 14-day avg | provisioned − consumed delta |
| Idle Classic/ALB/NLB (zero requests 14 days) | CloudWatch `RequestCount`/`ActiveFlowCount` | full LB cost |

**Cost model.** A static table in `internal/costs/prices.go` of *order-of-
magnitude* on-demand us-east-1 prices, clearly labeled "estimate"; regional
price differences are out of scope v1. No Pricing API dependency (it needs no
auth but is slow and complex); revisit if estimates need precision.

**UX.**

```bash
aws_explorer audit --only cost            # findings table + total at bottom
aws_explorer audit --only cost -o json    # machine-readable, each finding has estMonthlyUSD
```

```
SEVERITY    RESOURCE        ISSUE                                       EST/MO   FIX
🟡 WARNING  vol-0abc (1TiB) Unattached EBS volume (gp2)                 $100.00  Snapshot then delete, or attach
🟡 WARNING  eipalloc-0x1    Elastic IP not associated                   $3.60    Release the address
                                                              TOTAL ≈ $135.60/mo
```

**Implementation.** Checks in `internal/findings/cost.go`; data collection
reuses/extends the EC2, ELBv2, DynamoDB collectors with a detailed pass.
CloudWatch-metric-based checks (idle LB, DynamoDB) are tier-2: skipped with a
note when `cloudwatch:GetMetricData` is denied.

**Acceptance criteria.**
- Each finding carries `EstMonthlyUSD float64` (0 when unknown) and the table
  shows a total.
- All price constants in one file with source comments.
- Running against an account with zero waste prints "no findings" not an
  empty table.

---

## Theme C — Change attribution & drift

### AXE-005 — CloudTrail "who changed this" {#axe-005}

> **Status: ✅ shipped** — CLI `aws_explorer trail <resource-id-or-arn> [--since 7d]` + the shared `internal/trail` package (#84); the summary TUI's `t` timeline uses the same package; the VPC `w` diff overlay's `t` annotates each change with its likely actor (`internal/vpctui/diffactors.go`).

**Problem.** The VPC snapshot diff (AXE precedent: `snapshotdiff.go`) answers
*what* changed; nothing answers *who/when*. This is the single most useful
fact in an incident.

**UX.**
- **TUI:** on any resource detail (summary TUI and VPC explorer), press `T` →
  overlay listing recent CloudTrail events for that resource:

```
CloudTrail — sg-0abc123 (last 90 days)
2026-06-11 14:02  AuthorizeSecurityGroupIngress  role/deploy-pipeline   203.0.113.7
2026-06-09 09:15  ModifySecurityGroupRules       user/alice             198.51.100.2
```

- **CLI:** `aws_explorer trail <resource-id-or-arn> [--since 7d]`.
- **Diff integration:** the VPC "what changed" overlay (`w`) gains a key to
  fetch trail events for the changed resources, annotating each diff line
  with the likely actor.

**Implementation.**
- `internal/trail/trail.go`: wraps `cloudtrail:LookupEvents` with
  `LookupAttributes: [{ResourceName: <id>}]`. LookupEvents covers the last 90
  days of management events, no trail/bucket setup required — state this
  window in the UI.
- LookupEvents is rate-limited (2 TPS); serialize calls and cap page count.
- Event summarization (event name, principal short-form, source IP) is a pure
  function over the `CloudTrailEvent` JSON → fixture-testable.

**IAM needed.** `cloudtrail:LookupEvents`.

**Acceptance criteria.**
- `T` works on every resource type that has a usable ID/name; resources with
  no events show "no management events recorded in the last 90 days".
- Denied permission degrades to a one-line note inside the overlay.

### AXE-006 — Account-wide inventory snapshot diff {#axe-006}

> **Status: ✅ shipped** — `summary --baseline` / `summary --diff` (`internal/acctsnap`), `D` key in the summary TUI (`d` was already taken by half-page-down; `b` inside the overlay re-baselines).

**Problem.** "What changed in this account since yesterday?" The summary
command already builds a merged-by-ARN inventory of everything
(`internal/summary`); baseline and diff it, exactly like the per-VPC `w`.

**UX.**

```bash
aws_explorer summary --baseline          # save snapshot
aws_explorer summary --diff              # diff live vs latest baseline
aws_explorer summary --diff -o json      # for automation
```

```
Changes since baseline 2026-06-11 09:00 — 3 added, 1 removed
+ lambda/function   new-payments-fn        us-east-1
+ ec2/instance      i-0abc (web-3)         us-east-1
- s3/bucket         old-logs-bucket        global
```

**Implementation.**
- Baselines as JSON under `~/.aws_explorer/account-snapshots/<account-id>/`
  (keyed by account + region scope), mirroring `vpc-snapshots/`.
- Diff = set comparison on ARN; for matched ARNs compare name/state/tags
  (volatile fields excluded, the lesson from the VPC diff).
- TUI: in the summary TUI, a `w` key with the same semantics as the VPC
  explorer.

**Acceptance criteria.**
- Diff is stable across runs when nothing changed (ordering deterministic,
  volatile fields excluded).
- Baselines record region scope; diffing with a different scope warns instead
  of reporting everything as removed.

---

## Theme D — Expiry & deprecation

### AXE-007 — `expiring` command {#axe-007}

> **Status: ✅ shipped** — `aws_explorer expiring` (`cmd/expiring.go`, `internal/expiry` with static EOL tables in `eol.go`).

**Problem.** Calendar-driven breakage (cert expiry, runtime deprecation,
version EOL) is 100% predictable and still pages people. One report, sorted
by days remaining.

**Sources.**

| Item | API | Threshold default |
|------|-----|-------------------|
| ACM certificates | `acm:ListCertificates` + `DescribeCertificate` (`NotAfter`) | warn ≤ 45d, crit ≤ 14d |
| IAM server certificates | `iam:ListServerCertificates` | same |
| Lambda runtime deprecation | static table of runtime EOL dates in code (updated per release) | warn ≤ 90d, crit past EOL |
| EKS cluster version end-of-support | static table of K8s EOL dates + `eks:DescribeCluster` | warn ≤ 90d |
| RDS pending maintenance | `rds:DescribePendingMaintenanceActions` | info, crit if `ForcedApplyDate` set |
| RDS CA certificate rotation | `DescribeDBInstances` `CACertificateIdentifier` vs current CA | warn |
| Secrets Manager rotation overdue | `ListSecrets` (`RotationEnabled`, `NextRotationDate` in past / `LastRotatedDate` stale) | warn |

**UX.**

```bash
aws_explorer expiring [--all-regions] [--within 90d] [-o json|csv]
```

```
DAYS  WHAT                          RESOURCE                       DETAIL
  -3  Lambda runtime deprecated     payments-fn (python3.8)        deprecated 2026-06-09 — update runtime
  12  ACM certificate expires       *.example.com (arn:...:cert)   in use by alb/web — renew or re-issue
  61  EKS version end of support    prod-cluster (1.29)            upgrade before 2026-08-12
```

**Implementation.**
- New `cmd/expiring.go`; collectors for ACM are new
  (`internal/services/acm/`), the rest extend existing collectors with a
  detailed pass.
- Static EOL tables live in `internal/findings/eol.go` with a `lastReviewed`
  comment and a unit test that fails when a table entry's date is in the past
  *and* older than the module's release — a nudge to keep them fresh.
- The Lambda runtime table is the only data that can go stale; document that
  it reflects the binary's release date.

**Acceptance criteria.**
- Sorted ascending by days remaining; already-expired items first, negative
  days shown.
- Each row says what breaks and the action to take.
- `-o json` includes ISO dates and the threshold that fired.

---

## Theme E — Account-wide audit

### AXE-008 — `audit` command (account-wide security linter) {#axe-008}

> **Status: ✅ shipped** — security category added to `aws_explorer audit` (15 `SEC-*` checks in `internal/findings/security.go`, collection in `internal/audit/security_collect.go`; both categories run by default, `--only` narrows; CLI + TUI, `--fail-on`/`--ignore`/SARIF all apply).

**Problem.** The VPC findings linter is the tool's best feature; security
issues outside VPC networking deserve the same treatment.

**Checks** (beyond AXE-003 IAM and AXE-004 cost, which run under the same
command):

| Area | Finding | Severity |
|------|---------|----------|
| S3 | Bucket policy/ACL public, or account/bucket Public Access Block off | 🔴 |
| S3 | Default encryption disabled · versioning off on buckets with delete access | 🟡 |
| EBS | Unencrypted volumes · account default-encryption off | 🟡 |
| EBS/RDS | **Publicly shared snapshots** | 🔴 |
| RDS | Instance `PubliclyAccessible` · storage unencrypted | 🔴 / 🟡 |
| EC2 | Instance allows IMDSv1 (`HttpTokens != required`) | 🟡 |
| EC2 | Security groups with sensitive ports open to 0.0.0.0/0 (reuse VPC check, account-wide) | 🔴 |
| Lambda | Function URL with `AuthType: NONE` | 🔴 |
| SQS/SNS | Resource policy with `Principal: *` and no condition | 🔴 |
| CloudWatch | Alarm in `INSUFFICIENT_DATA` > 7 days (broken monitoring) | 🔵 |

**UX.**

```bash
aws_explorer audit [--all-regions] [--only iam,cost,s3,...] [--fail-on critical] [-o table|json|sarif]
```

Renders the findings table grouped by severity; `--fail-on` ties into AXE-023.

**Implementation.**
- This is the feature that forces the `internal/findings` extraction:
  `Finding` gains `ID` (the check's stable identifier, e.g. `S3-PUBLIC-001`),
  `Service`, `Region`, `ARN`, and `EstMonthlyUSD` fields; the VPC linter's
  type aliases to it.
- Checks are organized one file per service in `internal/findings/`; each
  check has a stable check-ID so suppressions (`--ignore S3-ENC-001`) and
  SARIF rules map cleanly.
- Data collection reuses collectors at `DetailLevelDetailed`, adding the
  per-resource describe calls each check needs (e.g.
  `GetBucketPolicyStatus`, `GetPublicAccessBlock`,
  `DescribeSnapshotAttribute`).

**Acceptance criteria.**
- Every check: pure function + fixture test (positive and negative case).
- A denied describe call skips that check with a note in the errors summary,
  never fails the run.
- Check IDs are stable and documented in the README.

### AXE-009 — Generalized where-used / blast radius {#axe-009}

> **Status: ✅ shipped** — `aws_explorer whereused <arn-or-id>` (CLI) + `internal/xref`. Pure, fixture-tested `Classify`/`BuildIndex`/`WhereUsed` over typed `Edge`s; best-effort collection (`internal/xref/collect.go`) reads the linking fields the inventory omits across Lambda, EC2 (instances/volumes/ENIs), RDS, Secrets Manager, SQS, ECS task defs, EKS clusters & node groups, ELBv2 listeners, and IAM (instance profiles + trust policies). "Not referenced" is scoped — every result lists the reference types checked. The summary TUI's `x` still uses its in-memory substring scan; replacing it with this typed index is a possible follow-up.

**Problem.** "Can I delete this?" The VPC explorer's `x` answers it for
networking; the most-asked non-networking targets are IAM roles, KMS keys,
security groups (account-wide), and certificates.

**Coverage v1.**

| Resource | References found via |
|----------|---------------------|
| IAM role | Lambda functions (execution role), ECS task/execution roles, EC2 instance profiles, EKS node groups, trust policies of other roles |
| KMS key | EBS volumes, RDS instances, S3 bucket default encryption, Secrets Manager secrets, SQS queues, Lambda env encryption |
| ACM certificate | ELBv2 listeners, CloudFront (note: requires global describe) |
| Security group | account-wide ENI attachments (extends the VPC version) |

**UX.** `x` on those resource types in the summary TUI (same overlay style as
vpctui's xref); CLI: `aws_explorer whereused <arn-or-id>`.

**Implementation.** `internal/xref` package: takes the collected inventory
(which already includes per-resource detail blobs) and builds an index of
ARN/ID references by scanning known fields. Where inventory lacks the linking
field, do targeted describes. Pure indexing function over inventory →
fixture-testable.

**Acceptance criteria.**
- "Not referenced by anything this tool can see" is an explicit answer, with
  the list of reference types that were checked (so absence of evidence is
  scoped, not implied as proof).

### AXE-010 — Relationship graph export (DOT / Mermaid) {#axe-010}

**Problem.** Architects want the picture. The xref index (AXE-009) is already
a graph; serialize it.

**UX.**

```bash
aws_explorer graph --vpc vpc-0abc -o mermaid > vpc.mmd
aws_explorer graph --focus arn:...:role/app --depth 2 -o dot | dot -Tsvg > app.svg
```

VPC explorer: a key in the export family writes the current VPC's graph next
to the Markdown export.

**Implementation.** `internal/graph`: node/edge model fed by the xref index
and the VPC snapshot; writers for DOT and Mermaid (`graph LR`, nodes grouped
by subgraph = subnet/service). Keep label text identical to table names so
the graph and TUI vocabularies match.

**Acceptance criteria.** Generated Mermaid renders on GitHub without edits;
DOT passes `dot -Tsvg` cleanly; snapshot-based golden-file tests.

---

## Theme F — Cross-navigation (TUI glue)

### AXE-011 — Jump from resource to its CloudWatch logs {#axe-011}

**Problem.** Four good TUIs that don't talk to each other. The most common
hop: resource → its logs.

**UX.** On a Lambda / ECS service / RDS instance / EKS cluster row or detail
panel, press `L` → switches to the cw TUI pre-filtered to the resource's log
group (`/aws/lambda/<fn>`, the ECS task definition's `awslogs` group,
`/aws/rds/instance/<id>/...`, `/aws/eks/<cluster>/cluster`). `Esc`/`q`
returns to where you were.

**Implementation.**
- `internal/cwtui` already accepts an initial group filter (`--group`);
  expose a constructor that takes group + region and returns the Bubble Tea
  model, then run it as a child program from the summary TUI (suspend/resume
  the parent — the same mechanism as shelling out, or embed the model and
  swap the root view; prefer embedding to keep one process and one terminal
  state).
- Log-group name derivation per service in a small pure function
  (`logGroupFor(resource) (string, bool)`) with tests.
- When the group doesn't exist, open the cw TUI with the name as a filter so
  the user sees "no groups match" in context rather than an error.

**Acceptance criteria.** Round-trip (jump in, `q` back) preserves the summary
TUI's selection, filters, and scroll position.

### AXE-012 — Open selected resource in the AWS console {#axe-012}

> **Status: ✅ shipped** — `internal/consolelink` (`URL`/`FromARN` + ARN-search fallback, `Open`/`CanOpenBrowser`); `o` in the summary TUI, VPC explorer, S3 browser, and CloudWatch logs TUI.

**Problem.** Sometimes you need the console. Generating the deep link is
pure string work and saves a minute of clicking every single time.

**UX.** `o` on any resource in any TUI: copies the console URL to the
clipboard (reuse `internal/ui/clip.go`) and, when a `$BROWSER`/OS opener is
available and the session is local, opens it. Status bar shows the URL
either way.

**Implementation.**
- `internal/consolelink`: `URL(arn string) (string, bool)` mapping
  service/type → console URL pattern
  (`https://<region>.console.aws.amazon.com/ec2/home?region=...#InstanceDetails:instanceId=...`).
  Start with the 15 supported services + the VPC explorer's resource types;
  fall back to the Resource Groups console ARN search URL for unknown types
  (works for almost anything): `https://console.aws.amazon.com/go/view?arn=...`.
- Table-driven tests: ARN in → URL out.

**Acceptance criteria.** Every resource type in the TUIs yields *some* valid
URL (specific or the ARN-search fallback); URL is correct for region-scoped
vs global services.

### AXE-013 — Global fuzzy finder {#axe-013}

> **Status: ✅ shipped** — `Ctrl+P` palette in the summary TUI (`internal/tui/finder.go`), scorer in `internal/fuzzy`, CLI twin `aws_explorer find` (`cmd/find.go`).

**Problem.** "I have `eni-0abc` from an error message — what is it?" Finding
it today means picking the right service first.

**UX.** `Ctrl+P` (or `:`) anywhere in the summary TUI opens a palette; typing
fuzzy-matches across **all** collected resources on name, ID, ARN, and type;
`Enter` jumps to that resource (selects its service, row, and opens the
detail panel). CLI twin: `aws_explorer find <fragment>` prints matches.

**Implementation.**
- The summary inventory is already in memory; matching is a scoring function
  (subsequence match like fzf's algorithm — implement a simple
  smith-waterman-ish scorer, no dependency) over `(name, id, arn)`.
- Render as the standard overlay with a live-filtered list (the quick-filter
  `/` machinery is the precedent).
- Cap displayed matches (~50) for responsiveness; matching 100k resources is
  a linear scan of in-memory strings — fast enough, verify with a benchmark.

**Acceptance criteria.** Matches on partial IDs without service prefixes;
result line shows name, type, region; `Enter` lands on the exact row with
detail open.

### AXE-014 — Inline CloudWatch metric sparklines {#axe-014}

**Problem.** "Is it healthy?" requires the console. One metric per resource
answers 80% of it.

**UX.** Detail panel gains a sparkline block (on demand — `m` key — not
auto-fetched, to keep scans API-frugal):

```
CPUUtilization (3h, 5m avg)   ▁▁▂▃▂▆█▅▃▂▁▂  now 12%  max 91%
```

Metric per type: EC2 `CPUUtilization`, Lambda `Errors`+`Throttles`, SQS
`ApproximateNumberOfMessagesVisible`, RDS `CPUUtilization`, ALB
`HTTPCode_Target_5XX_Count`, DynamoDB throttles.

**Implementation.** One `cloudwatch:GetMetricData` call per press (batch the
2–3 metrics for the resource into one call); render with the 8-step block
characters; pure renderer over `[]float64` with golden tests. Degrade to a
note when denied.

**Acceptance criteria.** Single keypress, single API call, sub-second render;
no metric data renders as "no datapoints" not an empty box.

---

## Theme G — Service-specific triage

### AXE-015 — ECS stopped-task triage {#axe-015}

> **Status: ✅ shipped** — `aws_explorer ecs stopped [--cluster]` (CLI). Pure classification in `internal/ecstriage/triage.go` (`Classify` + `exitNote`, fixture-tested), collection in `internal/ecstriage/collect.go` (`ListTasks(desiredStatus=STOPPED)` + `DescribeTasks`). Exit codes are glossed (137 → possible OOM-kill, 139 → segfault, 143 → SIGTERM, 134 → SIGABRT) and a container reason mentioning memory is treated as a stronger OOM signal. The TUI stopped-tasks view remains a possible follow-up.

**Problem.** "Why did my task stop?" is a perennial ticket; the answer
(`stoppedReason`, container exit codes) is one API call away but buried.

**UX.** On an ECS service/cluster in the TUI, a `stopped tasks` view (and CLI
`aws_explorer ecs stopped --cluster X`):

```
STOPPED AT         TASK        REASON                                        EXIT
2026-06-12 01:14   3f9a…       Essential container in task exited            137 (OOM?)
2026-06-12 01:10   77b2…       CannotPullContainerError: pull rate limit     -
```

Exit code 137 annotated as possible OOM-kill; 139 segfault; OOM flag from
container `reason` when present.

**Implementation.** Extend `internal/services/ecs/collector.go`:
`ListTasks(desiredStatus=STOPPED)` + `DescribeTasks` (stopped tasks are
retrievable for ~1 hour — say so in the UI when the list is empty). Reason
classification = pure function with fixtures.

### AXE-016 — Lambda triage view {#axe-016}

**Problem.** The classic silent Lambda failures: disabled event source
mapping, reserved concurrency 0, growing DLQ.

**UX.** Lambda detail panel gains a "Triage" section (fetched on demand):

```
Triage
  Event source mappings   1 ENABLED (sqs my-queue) · 1 DISABLED ⚠ (kinesis stream-x)
  Reserved concurrency    0 ⚠ — function cannot execute
  DLQ                     arn:...:dead-letters — 142 messages waiting ⚠
  Last 24h                errors 13 · throttles 4   (CloudWatch)
```

**Implementation.** `ListEventSourceMappings`,
`GetFunctionConcurrency`, DLQ depth via SQS `GetQueueAttributes` when the
DLQ is an SQS ARN, errors/throttles via one `GetMetricData`. The ⚠ rules
are findings-style pure functions and also surface in `audit` (AXE-008).

### AXE-017 — Service-quota dashboard {#axe-017}

**Problem.** Quota exhaustion causes the most mysterious failures ("can't
launch instances"). The VPC linter already does this for VPC limits
(`checkQuotas`); generalize with real quota values instead of hardcoded
defaults.

**UX.**

```bash
aws_explorer quotas [--all-regions] [--threshold 80]
```

```
QUOTA                                    REGION      USED / LIMIT     %
Running On-Demand Standard instances     us-east-1   58 / 64 vCPU    91% 🟡
VPCs per region                          us-east-1   5 / 5          100% 🔴
```

**Implementation.** `servicequotas:ListServiceQuotas` for limits (falls back
to `ListAWSDefaultServiceQuotas`, so account-specific increases *are*
reflected — fixing the documented limitation of the VPC checks); usage from
the quota's `UsageMetric` via CloudWatch where AWS provides one, else from
counts the collectors already gather (VPCs, EIPs, IGWs, rules/SG…). Start
with a curated list (~20 quotas that actually page people: vCPUs, EIPs,
VPCs, ENIs, Lambda concurrent executions, RDS instances, EBS storage)
rather than dumping thousands.

### AXE-018 — SQS/SNS plumbing checks {#axe-018}

> **Status: ✅ shipped** — `messaging` category in `aws_explorer audit` (5 `MSG-*` checks in `internal/findings/messaging.go`, collection in `internal/audit/messaging_collect.go`). Note: the SNS API does not expose pending-subscription age, so the ">24h" qualifier became a caveat in the finding text instead.

**Problem.** Broken async plumbing is silent: producers fill a queue nobody
consumes; redrive points at a deleted DLQ; SNS subscriptions stuck pending.

**Checks** (join `audit`):

| Finding | Detection | Severity |
|---------|-----------|----------|
| Queue has messages but no consumers | `ApproximateNumberOfMessages > 0` and `NumberOfEmptyReceives`/`ApproximateNumberOfMessagesNotVisible` ≈ 0 over 24h | 🟡 |
| Redrive policy targets a nonexistent queue | `RedrivePolicy` ARN not in collected queues | 🔴 |
| DLQ with messages | depth > 0 on a queue that is someone's redrive target | 🟡 |
| SNS subscription `PendingConfirmation` > 24h | `ListSubscriptionsByTopic` | 🟡 |
| SNS topic with zero subscriptions | subscription count | 🔵 |

**Implementation.** Extends SQS/SNS collectors with attribute fetches;
checks in `internal/findings/messaging.go`.

---

## Theme H — VPC path tracer completeness

These close the two *documented* limitations of the tracer
(`internal/vpctui/pathtrace.go`), removing its caveat lines.

### AXE-019 — IPv6 evaluation {#axe-019}

**Scope.** Accept IPv6 destination addresses; evaluate `::/0` and IPv6 CIDR
rules in SGs and NACLs; route lookup over `DestinationIpv6CidrBlock`
(longest-prefix on 128-bit); egress-only internet gateway as a valid internet
path (no public-IP requirement — IPv6 is publicly routed, EIGW is the
stateful filter). ENI matching against IPv6 addresses.

**Implementation.** The snapshot structs already exist; add IPv6 fields where
missing (SG rules carry `Ipv6Ranges`, routes carry the v6 CIDR). The
evaluation order doesn't change — each hop's matcher gains a v6 branch. Use
`netip.Prefix` throughout (the v4 code's `net` usage can migrate
opportunistically). Fixture tests mirror the v4 suite.

### AXE-020 — Managed prefix-list expansion {#axe-020}

**Scope.** When a rule or route references `pl-…`, resolve it:
AWS-managed lists (e.g. S3, DynamoDB) and customer-managed lists via
`ec2:GetManagedPrefixListEntries`, cached per trace. The hop verdict then
evaluates the actual CIDRs instead of flagging a caveat. If the get call is
denied, keep today's caveat behavior.

**Implementation.** Fetch lazily on first `pl-` encounter during snapshot
build; store `map[plID][]netip.Prefix` in the snapshot so trace evaluation
stays pure. Also benefits the findings linter (a "sensitive port open to
0.0.0.0/0" hidden inside a prefix list is currently invisible) and
plain-English explanations ("prefix list pl-123 (com.amazonaws.us-east-1.s3,
3 CIDRs)").

---

## Theme I — Multi-account & automation

### AXE-021 — Multi-account scanning {#axe-021}

**Problem.** Real orgs are 10–500 accounts; the tool is single-credential per
run (the TUI's `P` switcher is sequential, not aggregated).

**UX.**

```bash
aws_explorer --profiles prod,staging,dev          # comma list
aws_explorer --profiles 'team-*'                  # glob over ~/.aws/config
aws_explorer summary --profiles prod,dev -o csv   # ACCOUNT column appears
```

All commands that scan gain the flag; every output gains an `ACCOUNT` column
(account ID + profile name) when >1 profile is in play; TUI sidebar/table
get the same column and the errors overlay attributes failures per account.

**Implementation.**
- `internal/auth/profiles.go` already enumerates profiles; add glob matching.
- The engine's task unit becomes (profile, service, region); `aws.Config` is
  built per profile up front (fail fast per profile, keep the rest).
- `model.Resource` gains `AccountID`/`Profile` fields (additive; JSON output
  documents them).
- Organizations-based enumeration (assume a role in every member account) is
  v2 — design the flag surface so `--org-role <name>` can slot in later
  without breaking `--profiles`.

**Acceptance criteria.** One bad profile (expired SSO, missing) is reported
per-profile and doesn't abort the others; concurrency bound still respected
globally, not per account.

### AXE-022 — SSO-aware auth errors {#axe-022}

> **Status: ✅ shipped** in #81 (`internal/awserr/expired.go`; wired into the engine, discovery and audit error paths).

**Problem.** An expired SSO session today surfaces as a raw SDK error string.
The fix is always the same command.

**UX.** Detect SSO/expired-token error classes
(`SSOProviderInvalidToken`, `ExpiredToken`, `InvalidGrantException`,
"token has expired") and print exactly:

```
✗ AWS session for profile 'prod' has expired.
  → run: aws sso login --profile prod
```

(or `--sso-session <name>` when the profile uses a shared sso-session block —
`internal/auth/profiles.go` can read which). In TUIs the same message shows
in the errors overlay / profile-switch panel, not just stderr.

**Implementation.** `internal/awserr` (exists) gains an
`IsExpiredSSO(err) (profile-fix string, bool)` classifier; call sites in
`internal/auth` and the engine error aggregation route through it. Pure
classifier over error strings/types → table-driven tests.

### AXE-023 — CI mode: exit codes, `--fail-on`, SARIF {#axe-023}

> **Status: ✅ shipped** in #80 (`--fail-on`, `--ignore`, `-o sarif`; check registry in `internal/findings/checks.go`).

**Problem.** The linters (AXE-003/004/008/018) become 10× more valuable as a
pipeline gate.

**UX.**

```bash
aws_explorer audit --fail-on critical             # exit 2 if any critical
aws_explorer audit --fail-on warning -o sarif > audit.sarif
aws_explorer audit --ignore S3-ENC-001,IAM-KEY-001
```

Exit codes: `0` clean (or below threshold), `2` findings at/above threshold,
`1` operational error. SARIF 2.1.0 output maps check-ID → rule, finding →
result (level: error/warning/note), ARN → logical location — uploadable to
GitHub code scanning.

**Implementation.** `internal/findings/sarif.go` (serializer + golden-file
test against the SARIF schema); `--fail-on`/`--ignore` handled in the shared
audit command plumbing. Requires stable check IDs (AXE-008).

### AXE-024 — Inventory caching / instant TUI start {#axe-024}

**Problem.** On large accounts an `--all-regions` sweep takes minutes; the
TUI is blank until then. Stale-but-instant beats fresh-but-later for
*orientation*, and the TUI already handles incremental arrival (streaming
collectors).

**UX.** TUI opens instantly with the last scan's data, visibly stamped
`cached 2026-06-12 09:14 — refreshing…`, and rows update in place as the
fresh scan streams in. CLI: `--cached` prints the cache without scanning;
`--no-cache` disables. Cache is per account+region-scope under
`~/.aws_explorer/cache/`, JSON (or gzip-NDJSON if size warrants), with a
configurable TTL (`app.cacheTTL`, default 24h — older caches are ignored).

**Implementation.** `internal/cache`: save the final merged inventory after
each successful scan; on TUI start, load + render, then run the normal scan
and reconcile by ARN (replace-on-arrival per service/region as each task
completes, so deleted resources disappear when their service@region task
finishes — not before). Secrets: cache stores the same fields the JSON output
already exposes, nothing extra; document the file location and that
`aws_explorer cache clear` removes it.

**Acceptance criteria.** Cold start unchanged; warm start renders < 100ms
after launch; a resource deleted in AWS disappears after refresh; cache
never used across different accounts/credentials (keyed by account ID).

---

## Cross-cutting requirements

Applies to every AXE item:

1. **Permissions documentation.** Each feature's README section lists the
   exact IAM actions it needs; denial of any action degrades that feature
   with a visible note (errors overlay / stderr summary), never a crash.
   Maintain a consolidated "minimum read-only policy" appendix in the README.
2. **Read-only guarantee.** Nothing in this roadmap mutates AWS. (AXE-002
   simulation, AXE-001 decode, and all describes/lookups are read-only.)
   Anything future that mutates follows the Reachability Analyzer
   confirmation pattern.
3. **Testing.** Every analysis/classification/rendering function is pure and
   fixture-tested, per the existing convention (`findings_test.go`,
   `pathtrace_test.go`). API wrappers are thin and mockable via interfaces as
   in current collectors.
4. **Config.** New tunables go under existing config sections
   (`app.cacheTTL`, `audit.ignore`, `expiring.within`) with embedded
   defaults; everything works with zero config.
5. **Key bindings.** New TUI keys (`T` trail, `L` logs, `o` console, `m`
   metrics, `Ctrl+P` finder) register through `internal/ui/keys.go` /
   `keyhints.go` so the context-aware status bar stays truthful; check for
   collisions per screen before assigning.
6. **README.** Each shipped feature gets a README section in the existing
   style (UX example block + flags table + caveats).

## Phasing {#phasing}

Suggested order, sized so each phase ships independently:

| Phase | Items | Rationale |
|-------|-------|-----------|
| **1 — Quick wins** | AXE-012 (console links), AXE-022 (SSO errors), AXE-001 (decode) | Small, isolated, immediately felt daily |
| **2 — Findings platform** | extract `internal/findings`, AXE-004 (cost), AXE-023 (CI mode) | Cost linter is the flagship; CI mode multiplies it |
| **3 — Incident story** | AXE-005 (CloudTrail), AXE-006 (account diff), AXE-013 (fuzzy finder) | "What changed, who changed it, jump to it" |
| **4 — Audit breadth** | AXE-008 (audit), AXE-003 (IAM lint), AXE-018 (SQS/SNS), AXE-007 (expiring) | Reuses phase-2 platform |
| **5 — Tracer & triage** | AXE-019, AXE-020, AXE-015, AXE-016, AXE-002 | Deepens debugging verticals |
| **6 — Scale & glue** | AXE-021 (multi-account), AXE-024 (cache), AXE-011 (log jump), AXE-017 (quotas), AXE-009/010 (xref/graph), AXE-014 (sparklines) | Larger structural work |

Dependencies: AXE-023 and AXE-008 need the `internal/findings` extraction
(phase 2). AXE-010 builds on AXE-009. AXE-024 benefits from AXE-021's
account-keying decisions but doesn't require it.
