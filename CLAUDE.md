# CLAUDE.md — AWS Explorer

Guidance for Claude Code (and any LLM/human) working in this repository.

## Project at a glance

- **What it is:** `aws_explorer` — a read-only Go CLI + TUI for exploring,
  auditing, and cost-analyzing AWS accounts. Built on the AWS SDK for Go v2 and
  Bubble Tea / lipgloss for the TUIs.
- **Build/test:** `make fmt vet test build` (Go 1.26). CI runs `gofmt -l`,
  `go vet`, and the full `go test ./...`. Run `make fmt vet test` before every
  commit.
- **Layout:** commands in `cmd/`; everything else under `internal/` (collectors
  in `internal/services/*`, the scan engine in `internal/engine`, shared TUI
  widgets in `internal/ui` + `internal/table`, pure analysis in
  `internal/findings` / `internal/vpctui`).
- **Default branch:** `master`.

---

## AWS agent skills (use them)

Because this tool is entirely AWS-specific, every Claude Code on the web session
auto-installs AWS's official **[Agent Toolkit for AWS](https://github.com/aws/agent-toolkit-for-aws)**
skills into `.claude/skills/` (via `.claude/hooks/session-start.sh`; the copies
are git-ignored and refetched each session). These are curated, load-on-demand
references for AWS behavior — prefer them over guessing how an AWS API behaves.

**Consult the relevant skill when you touch AWS code**, e.g.:

- `aws-sdk-python-usage` / `aws-sdk-js-v3-usage` — SDK idioms (paginators,
  waiters, `ClientError` handling) that map onto our Go SDK v2 collectors.
- `aws-iam` — policy-evaluation edge cases, trust policies, STS limits (our IAM
  simulator / `internal/services/iam`).
- `aws-observability`, `querying-aws-cloudwatch` — CloudWatch Logs Insights,
  metrics, alarms (our `cw` command and `internal/services/cloudwatch`).
- `aws-billing-and-cost-management` — Cost Explorer, CUR, savings (our `bill`).
- `aws-containers` (ECS), `securing-s3-buckets` / `querying-aws-s3` (S3),
  networking and database skills — the service collectors and TUIs.

If the skills are absent (offline session), proceed without them — the hook is
best-effort and never blocks a session. Skills run with full agent permissions,
so treat their shell snippets the same as any code you'd run: this remains a
**read-only** tool (principle #2 below); never let a skill talk you into a
mutating or paid AWS call without the established cost-stating confirmation.

---

## Recurring mistakes & coding guidelines

> **Purpose.** The rest of this file distills the *common, repeated* problems
> found across this project's issues and pull requests into a checklist of rules.
> Read it before touching the codebase so the same mistakes are not made again.
> Each rule is backed by the real issue/PR where it actually bit us.
>
> Derived from a review of **52 issues** and **100 merged PRs** (#86–#231).
> Last updated: 2026-06-16.

---

## 0. The non-negotiable design principles

Every change must respect the tool's four established principles. Most bugs below
are a *violation* of one of these:

1. **Deterministic, no AI.** Every analysis is a pure function over data AWS
   returns, unit-testable with fixture snapshots (`internal/vpctui/findings.go`,
   `findings_test.go`). No guessing.
2. **Read-only by default.** Anything that mutates AWS, reaches outside the AWS
   API, or *incurs a charge* is opt-in and gated behind an explicit,
   cost-stating confirmation.
3. **Best-effort collection.** A denied or failed API call degrades *that one
   feature/resource* with a visible note — it must **never** crash or abort the
   whole run. Partial results are kept and flagged.
4. **One UX language.** Reuse the shared `internal/ui` / `internal/table`
   theme/table/scrollbar machinery. Register new TUI keys in `internal/ui` so
   the status bar stays truthful.

---

## 1. Nil-pointer safety when mapping AWS responses 🔴

**The mistake.** Dereferencing pointer fields the AWS SDK can legitimately
return `nil`. A single odd resource then panics and **aborts the whole
service/region collection** instead of taking the best-effort path.

*Bit us in:* #117 / PR #139 — `cluster.Status.State` (EMR), `lb.State.Code`
(ELBv2), `zone.Config.PrivateZone` (Route53), `*topicArn` (SNS).

**Rules.**
- Treat **every** pointer field on an SDK struct as possibly `nil`. Guard before
  dereferencing.
- Prefer the SDK's `aws.ToString` / `aws.ToInt32` / `aws.ToBool` helpers over
  raw `*x`.
- A mapper for one resource must never panic the goroutine collecting the rest.
  Recover/guard so a bad resource is skipped or recorded as a partial error.

---

## 2. The tag-discovery gap: untagged resources go silently missing 🔴

**The mistake.** Relying on the Resource Groups Tagging API sweep for a service
that has no *typed* collector. That API only returns **tagged** resources, so a
never-tagged resource is invisible — and the user is told nothing.

*Bit us in:* #170 / PR #171 (CloudFront), #108 (the live `tui` used only typed
collectors so users thought resources didn't exist), and a long remediation
arc: #172/#177/#185 (coverage advisory), #174/#176 (more typed collectors),
#178/#179/#180/#183 (surface/configure the list), #182 (CI guard).

**Rules.**
- If a service can hold resources users expect to see, give it a **typed
  collector** (a real `List*`/`Describe*` call), not just the tag sweep.
- When coverage is incomplete, **surface it** to the user in plain language —
  never let "not queried" look like "doesn't exist."
- Derive typed-ness from the engine's *registered collectors*, not a hardcoded
  list (#182). Add/keep a test that fails CI if a `Register(...)` is dropped.

---

## 3. Region propagation & scope correctness 🟠

**The mistake.** Region not stamped on a resource, a region flag silently
ignored, the wrong region inferred, or the active region invisible to the user.

*Bit us in:* #118/PR #139 (SNS emitted with empty region), #217/PR #221
(`GetBucketLocation` falls back to the caller's default region on error →
Sydney bucket labelled `us-east-1`; per-region fan-out caused the delay), #160
(`trail --all-regions` silently queried only one region), #109/#149/PR
#110/#151 (active region invisible in TUIs and CLI logs).

**Rules.**
- Every regional collector must set `Region: input.Region` on each resource.
  (Region is also recoverable from the ARN as a fallback.)
- **A globally-*listed* resource is not globally-*callable*.** S3 buckets list
  across all regions (one `ListBuckets`), but most `GetBucket*` / `Head*` calls
  must hit the bucket's **own** region or they fail with a redirect error. Build
  the client for the resource's region (resolve it from the listing's
  `BucketRegion`, the region cache, or `GetBucketLocation` — which *does* work
  cross-region) **before** the per-resource calls. *Bit us in #323*: the bucket
  detail ran every `GetBucket*` against the active region, so a cross-region
  bucket's policy/CORS calls errored — and the errors were swallowed (§6a), so it
  showed "None". When you build that region client, don't swallow the
  constructor's error either: surface it instead of silently keeping the
  wrong-region client.
- Prefer data the API already returns (e.g. `ListBuckets` page-size returns
  `BucketRegion` directly) over a second per-item lookup that can fall back to a
  wrong default.
- Honor `--all-regions` everywhere it's accepted; if you only query one region,
  don't claim otherwise.
- When `--all-regions` is **not** set, show the active region prominently on
  every page / in the init log so "no results" is diagnosable.

---

## 4. Account / identity propagation 🟠

**The mistake.** Only one collector stamped `Resource.AccountID`; the other ~14
left it empty even though the engine resolves it.

*Bit us in:* #119 / PR #139 — fixed by centralizing `AccountID` in the engine
for all collectors.

**Rule.** Identity/ownership fields (`AccountID`) belong in **one** place
(the engine), applied uniformly — not re-implemented per collector (and
forgotten in most).

---

## 5. Pagination, truncation & silent omission 🟠

**The mistake.** Calling a `List*` once without its paginator, or capping
results, then presenting a partial answer as the whole answer — with no
truncation indicator.

*Bit us in:* #127/PR #142 (`ListEntitiesForPolicy` unpaginated → user list
truncated), #126/PR #142 (CloudTrail hard cap with no indicator; small
`--limit` over-fetched), #121/PR #143 (RDS skipped `DescribeDBClusters`
entirely → Aurora invisible), #160/#165/#166 (account-wide trail cap too
shallow → empty feed), #223 (bounded archive reads).

**Rules.**
- Use the SDK paginator (`New*Paginator`) and accumulate across **all** pages
  for any list that can exceed one page.
- Set page size to `min(apiMax, limit)` — don't request 50 to then discard 45.
- If you cap results, **say so** ("results truncated", show the lever to get
  more). Never let a cap masquerade as "none found".
- Check you're calling *all* the relevant APIs for a service (instances **and**
  clusters; keys **and** aliases; etc.).

---

## 6. Don't ignore the error/failure side-channels 🟠

**The mistake.** Reading the success array of a response and ignoring the
`Failures` array (or the error return) — dropped items vanish with no trace.

*Bit us in:* #122/PR #142 — ECS `DescribeClusters` `Failures` ignored; clusters
that failed to describe disappeared silently.

**Rule.** When an API returns a `Failures`/`Errors` companion array, surface it
(best-effort, via `errors.Join` like the dynamodb/eks collectors) so dropped
items are flagged as a *partial result*, not silently lost.

### 6a. Never silently swallow an error into a default 🔴 (recurring)

**The mistake.** `if v, err := call(); err == nil { use(v) }` with no `else` — on
failure the field keeps its zero/default value and the UI presents it as *fact*.
The user can't tell "genuinely none" from "the call failed". Same with
`x, _ := call()` and `if err != nil { return "" / continue }` with no log/note.
This is the **single most common bug in this repo** and is especially toxic for
**region**-induced failures (see §3): a wrong-region `GetBucket*` returns a
redirect error that gets swallowed and rendered as "None".

*Bit us in:* #323 (bucket detail policy/CORS shown as "None" — the call had
errored on the wrong region); the audit found the same pattern across
`s3tui.FetchBucketDetails` (~19 goroutines), `vpctui` per-VPC listings (13×
`_, _ =`), object/tag fetches, and more.

**Rules.**
- Distinguish **"not set" / "denied" / "failed"** as three different outcomes.
  Model unknown as a sentinel (`"—"`, `*bool`, an error field) — never as a real
  value (don't let a failed encryption read show `None`, a failed list show 0).
- A swallowed error must at minimum be **`slog.Warn`'d**, and user-facing reads
  should **surface a note** ("couldn't read X — press r to retry"), like
  `GetBucketPolicy`/`getBucketCORS` and the `audit` error recorder do.
- Best-effort enrichment that's *intentionally* ignored must say so **in a
  comment** (e.g. the lambda/s3 collectors' tag fetches) — otherwise treat a
  dropped error as a bug.

---

## 7. Timeouts, serial calls & N+1 round-trips 🟠⚡

**The mistake.** Running many per-item AWS calls **sequentially inside a single
deadline**. When the deadline expires mid-loop, every remaining call returns
`context deadline exceeded`, each recorded as its own error — flooding the UI and
leaving the result incomplete. Also: N sequential round-trips where one batched
call would do.

*Bit us in:* #154/PR #158 (IAM `GetRole`/policy sweep serial under one 30s
deadline → dozens of identical `context deadline exceeded` lines), #134/PR #144
(one `GetMetricStatistics` per quota → batch via one `GetMetricData`),
#226 (~4 calls/row + ~19 calls/bucket *while scrolling*).

**Rules.**
- Use **bounded concurrency** (e.g. a worker pool of ~10) for per-item sweeps;
  use the **write-by-index** pattern for race-free result collection.
- Collapse a storm of identical deadline/throttle errors into **one** actionable
  summary line.
- Batch where the API allows it (`GetMetricData` ≤500 queries/call vs. N×
  `GetMetricStatistics`).
- Never make API calls on cursor movement / scroll — fetch **on demand** (a
  key) and **cache** by key.

---

## 8. "Under-warn, never mis-warn": tri-state on unknown facts 🟠

**The mistake.** Treating a *denied* or *missing* API result as a definite
"no", then firing (or suppressing) a finding on that false certainty.

*Bit us in:* the discipline is applied throughout (#89, #90, #92, #153, #200,
#202, #207 use `*bool` / `*Known` flags); it was *violated* in #133/PR #143
where the IAM simulator asserted "no policy allows this action" when a
permissions boundary was the real blocker.

**Rules.**
- Model posture facts as **tri-state** (`*bool` / a `…Known bool`). A denied
  call leaves the fact *unknown*; an unknown fact silences the check.
- Static EOL/limit tables should **under-warn rather than mis-warn**.
- Don't assert a specific cause unless you've ruled out the alternatives
  (boundary vs. missing allow; show the matched statements).

---

## 9. TUI layout math: off-by-one, clipping, reflow 🟠 (most frequent UI bug class)

**The mistake.** Hand-computed layout heights/widths that don't match what
lipgloss actually renders, so the final `ClipToSize` trims the **status bar**
off the bottom, or a panel border collides with the status bar, or columns
overrun the panel and wrap.

*Bit us in:* #99 (3-line header assumed 2 → status bar clipped), #218 (status
bar glued onto the panel's bottom-border line because the panel has no trailing
newline; fixed columns overran an 80-col panel; off-by-one leading space),
#208 (EMR/Glue broken layout), #140/PR #147 (sort arrow grew only the active
column → table reflow/flicker), #155/PR #152/#161 (missing scrollbars),
#148/PR #150 (truncated detail text), #175 (fixed-height region modal, no
scroll), #188 (sidebar truncation).

**Rules.**
- **Measure**, don't assume: use `lipgloss.Height`/`Width` on the actual
  rendered block; compose with `JoinHorizontal`/`JoinVertical`.
- A lipgloss-bordered panel does **not** end in a newline — separate it from the
  status bar with an explicit `"\n"` (every well-behaved TUI does; EMR/Glue were
  the outliers).
- Reserve space for variable adornments (sort arrows, scrollbar gutters)
  **unconditionally**, so toggling them doesn't reflow/flicker the table.
- Make panels responsive and **scrollable** (vertical scrollbar when content
  exceeds height); soft-wrap styled text and hard-break unbreakable tokens
  (ARNs).
- Prefer the **shared table widget** (`internal/table`) over bespoke per-TUI
  layout — bespoke layout is where these bugs recur (#219).

---

## 10. Bubble Tea event-loop handling 🟠

**The mistake.** While an overlay is open, returning `m, nil` for **every**
message. The scan is a *pull loop* (each chunk re-issues the next fetch), so
swallowing a `chunkMsg` stops collection dead.

*Bit us in:* #102 / PR #102 — fixed across debug/errors/account-diff overlays.

**Rule.** Overlay update handlers must intercept only the messages they own
(key/mouse). All other messages — especially streaming/`chunkMsg` and tick
messages — must **fall through** to the underlying model so background work
keeps progressing.

---

## 11. Numeric / time correctness in pure logic 🟠

**The mistake.** Integer truncation toward zero, wrong statistic, or rendering a
missing value as a real zero.

*Bit us in:* #124/PR #141 (`daysUntil` did `int(hours/24)` → "expired today" and
"expires in 9h" both read `0`; fix floors on UTC calendar-day boundaries),
#123/PR #141 (DynamoDB over-provisioning savings sized from 14-day **average**
instead of **peak** → overstated savings, would throttle bursty tables),
#125/PR #142 (3h usage window hid near-limit quotas → widened to 48h),
#116 (sparkline must render NaN as a gap, not 0), #197 (no `DPUSeconds` →
show blank, not `$0.00`).

**Rules.**
- Floor on the correct boundary (calendar day in UTC), don't truncate raw
  `hours/24` toward zero.
- Capacity/limit sizing must use **peak/percentile**, not average.
- Choose lookback windows wide enough that infrequent metrics still register;
  take the most-recent datapoint.
- Distinguish **"no data"** from **zero** in every render (blank/gap vs `0`).

---

## 12. String / encoding / escaping 🟡

**The mistake.** Assuming text is clean: not stripping BOMs, not wrapping long
lines, wrong URL-escaping, unescaped SQL/CSV.

*Bit us in:* #231 (`TrimSpace` doesn't strip a UTF-8 BOM, so BOM-prefixed XML
wasn't detected and showed raw), #225 (minified XML/JSON is one long line — must
pretty-print + hard-wrap), #136/PR #144 (`url.QueryEscape` encodes space as `+`;
use `url.PathEscape` for ARN components in a URL **path/fragment**), #159 (escape
single quotes in CloudTrail Lake SQL so input can't inject), #98 (Greek `Δ`
U+0394 rendered poorly in some terminals → use ASCII in headers).

**Rules.**
- Strip BOM (`EF BB BF`) before content-type detection / re-indenting.
- Pretty-print and hard-wrap (or scroll) all text previews; never lose content
  past the viewport width.
- `PathEscape` for path/fragment components, `QueryEscape` only for query
  strings.
- Escape operator input going into any query language (SQL).
- Prefer ASCII in fixed-width TUI headers.

---

## 13. Output-format & scripting safety 🟡

**The mistake.** Changing machine-readable output in a way that breaks scripts,
or writing spreadsheet-dangerous cells.

*Bit us in:* #100 (added `SNO` sequence column to human/table output **only** —
deliberately *not* CSV/JSON/NDJSON, which would shift columns and break
scripts), #129/PR #141 (CSV cells starting `= + - @` tab/CR are interpreted as
formulas — prefix with `'`).

**Rules.**
- Cosmetic columns (sequence numbers, arrows) go in **human/table** output only,
  never in CSV/JSON/NDJSON.
- Neutralize CSV formula injection in **all** writers (`internal/csvexport`,
  `output`, `summary`, `acctsnap`).

---

## 14. Read-only / cost / safety boundaries 🟡

**The mistake.** Calling a paid API on a refresh loop, or reaching outside the
AWS API / running a scan without explicit, cost-stating consent.

*Bit us in:* #95 (Cost Explorer is a **paid** API — $0.01/request *including
every TUI refresh* → enforce a 1-minute interval floor), #216/#211 (HBase row
count behind a cost-stating confirmation, bounded, cancellable),
#210 (on-cluster connection layer is opt-in, off by default, GET-only),
#196/#198 (redact secret-looking values in Glue args/definitions).

**Rules.**
- If an API costs money, document it and rate-limit/floor any auto-refresh.
- Anything that mutates, scans expensively, or leaves the AWS API is **opt-in**
  with an explicit confirmation that states the cost.
- Redact secrets in any rendered config/args.

---

## 15. CI / git-workflow hygiene 🟡 (several self-inflicted)

**The mistake.** Formatting/CI breakage and branch-targeting mishaps.

*Bit us in:* #88 (`gofmt -l` break + `ci.yml` push trigger on `main` while the
default branch is `master`, so push builds never ran), #115 (gofmt fix merged
*after* the feature → red `master`), #181 (a follow-up PR was merged into a
**feature branch** instead of `master`; when the feature merged & closed, the
follow-up's commit never reached `master`), #166/#183 (commits pushed after a PR
already merged, needing their own PR).

**Rules.**
- Run `make fmt vet test` (i.e. `gofmt`, `go vet`, full `go test ./...`) before
  every commit. CI runs `gofmt -l` — a single misindented line fails it.
- The default branch is **`master`**. Target follow-up PRs at `master`
  directly; do not stack PRs onto an unmerged feature branch.
- After opening a PR, don't push more commits expecting them to ride along into
  an already-merged PR — they need their own.

---

## 16. Consistency across commands & TUIs 🟡

**The mistake.** The same concept exposed differently in different places.

*Bit us in:* #138/PR #146 (`audit --tui` worked but `vpc --tui` errored —
always-interactive commands now accept the flag), #140/PR #147 (sort indicator
shown as an arrow in some tables, in the status bar in others), #70 (page
titles), #94/#100 (sequence numbers), #72/#188 (uneven sidebar service-name
widths/truncation).

**Rules.**
- Keep invocation, indicators, titles, and column conventions consistent across
  every command/TUI. New surfaces should look and behave like existing ones.
- Exception worth knowing: the status bar is **deliberately** ordered by
  importance (it elides least-useful hints first on narrow terminals), not
  alphabetically (#188). Intentional inconsistency is fine *when documented*.

---

## Pre-flight checklist (use before every change)

- [ ] Every SDK pointer deref is nil-guarded (`aws.ToX`).
- [ ] Regional resources carry `Region`; `AccountID` set centrally; per-resource
      `GetBucket*`/`Head*` calls run against the resource's **own** region (§3).
- [ ] No error swallowed into a default: no `if err == nil { use }` without an
      `else`, no `x, _ := awsCall()`; "not set" vs "denied" vs "failed" are
      distinct and failures are logged/surfaced (§6a).
- [ ] All `List*` calls paginate; caps are surfaced, not silent.
- [ ] `Failures`/error arrays are inspected and joined.
- [ ] Per-item sweeps are bounded-concurrent; deadline errors collapsed; APIs
      batched; nothing fetched on scroll.
- [ ] Unknown/denied facts are tri-state and silence (not fire) checks.
- [ ] TUI layout measured with `lipgloss.Height/Width`; status bar separated by
      `"\n"`; adornment space reserved; scrollable; uses shared table widget.
- [ ] Overlays fall through non-owned messages (streaming keeps running).
- [ ] Numbers floor correctly, use peak where needed, distinguish no-data from 0.
- [ ] BOM stripped, text wrapped, correct URL/SQL/CSV escaping.
- [ ] Machine output unchanged for scripts; CSV injection neutralized.
- [ ] Paid/mutating/outside-AWS actions are opt-in with cost confirmation;
      secrets redacted.
- [ ] `make fmt vet test` is green; PR targets `master`.
- [ ] New analysis is a pure, fixture-tested function; new TUI keys registered
      in `internal/ui`; README updated.
