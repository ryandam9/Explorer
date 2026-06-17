# AWS Lambda Support — Design Specification

Status: **Shipped** · Theme K of the [enhancement roadmap](enhancement-roadmap.md)
· Tracking issue: [#304](https://github.com/ryandam9/aws_explorer/issues/304)

This document specifies a first-class AWS Lambda feature set for `aws_explorer`.
It continues the roadmap's stable-ID scheme (`AXE-NNN`) under a new theme so the
work can be referenced unambiguously in commits and PRs (e.g.
`AXE-046: lambda interactive dashboard TUI`). It also subsumes the older
roadmap item **[AXE-016 — Lambda triage view](enhancement-roadmap.md#axe-016)**,
which sketched a function "Triage" panel; the dashboard below delivers the
runtime/health/DLQ signals AXE-016 wanted as first-class tabs, a detail panel
and a findings panel.

It follows the tool's established design principles:

1. **Deterministic, no AI** — every analysis is a pure function over data the
   Lambda API returns, unit-testable with fixture snapshots (the pattern set by
   `internal/findings/glue.go` and `internal/findings/messaging.go`).
2. **Read-only by default** — the entire feature set only *describes* Lambda;
   nothing invokes, updates or mutates a function, layer or mapping.
3. **Best-effort collection** — a denied Lambda API call degrades a feature,
   never crashes it; partial results are kept and flagged (the `Collector`
   contract in `internal/services/service.go`, and the per-region soft-failure
   gather in `internal/lambdatui/client.go`).
4. **One UX language** — every new table/overlay/detail panel uses the shared
   theme/table/key-hint machinery in `internal/ui` and `internal/table`;
   findings render in the existing severity/resource/issue/fix style.

---

## Why Lambda, and what was there before

AWS Lambda is the account's serverless compute layer. Engineers debug it from
the console all day:

> "What runtime is this on — is it deprecated?" · "Why isn't this function
> invocable?" · "Where are the logs?" · "What memory/timeout/role is it actually
> configured with?" · "Which event source feeds it, and is the mapping
> enabled?" · "Does it have a dead-letter queue?"

**Prior state.** `internal/services/lambda/collector.go` already listed
functions into the account-wide inventory (`ListFunctions` + per-function
`ListTags`), so functions appeared in `summary`, the fuzzy finder, `whereused`,
snapshot diffs and the console-link `o` fallback; `internal/expiry` already
flagged deprecated runtimes in the `expiring` watchlist. What was missing was
**everything interactive and operational**: a Lambda dashboard, the function
configuration at a glance, layers, event-source mappings, a log jump, and any
Lambda-aware `audit` findings. This document closes that gap.

### Mapping Lambda to the tool's surfaces

| Lambda concept | API (read-only) | Where it lands |
|----------------|-----------------|----------------|
| Function list + config | `ListFunctions` | inventory + Functions tab (AXE-045/046) |
| Function detail | `GetFunction` | detail overlay (AXE-047) |
| Layer | `ListLayers` | Layers tab (AXE-046) |
| Event-source mapping | `ListEventSourceMappings` | Event sources tab (AXE-046) |
| Function logs | CloudWatch `/aws/lambda/<name>` | `L` log jump (AXE-048) |
| Health / EOL | derived from the above | `audit --only lambda` (AXE-049) |
| Deep links | pure string mapping | `o` in every Lambda surface (AXE-050) |

---

## Contents

| ID | Title | Status |
|----|-------|--------|
| [AXE-045](#axe-045) | Lambda collector (already lists functions) | ✅ pre-existing |
| [AXE-046](#axe-046) | `lambda` interactive dashboard TUI (Functions / Layers / Event sources) | ✅ shipped |
| [AXE-047](#axe-047) | Function configuration detail overlay | ✅ shipped |
| [AXE-048](#axe-048) | Jump from a function to its CloudWatch logs | ✅ shipped |
| [AXE-049](#axe-049) | `lambda` audit category (runtime / health findings) | ✅ shipped |
| [AXE-050](#axe-050) | CLI twins + Lambda console deep links | ✅ shipped |

---

## Architecture context

Where the new code plugs in (all paths confirmed against the current tree):

- **TUI** — a new Bubble Tea package `internal/lambdatui`, modelled on
  `internal/gluetui` (model/update/view, `NewModel` factory, tab enum,
  `textinput` filter, spinner, toast, scrollable detail overlay, findings panel)
  and built from `internal/table` + `internal/ui`. The command (`cmd/lambda.go`)
  mirrors `cmd/glue.go`.
- **Client** — `internal/lambdatui/client.go` holds one `lambda.Client` per
  region and gathers the three listings concurrently per region, merging with a
  soft per-region failure model (an error only when *every* region fails).
- **Detail** — `GetFunction` is fetched on demand when `Enter` is pressed on a
  function; layers and event-source mappings render synchronously from the
  loaded list data.
- **Log jump** (AXE-048) reuses the `cmd/glue.go` `tea.ExecProcess` pattern: it
  runs `aws_explorer cw --group /aws/lambda/<name>` as a child, inheriting
  `--region` / `--profile` / `--config`. The group derivation matches
  `internal/loggroup` (which already supports Lambda).
- **Findings** (AXE-049) add `internal/findings/lambda.go` — a `LambdaSnapshot`
  struct + pure `AnalyzeLambda(snap) []Finding`, check-ID constants registered
  in `internal/findings/checks.go`; the category is wired into `cmd/audit.go`'s
  `auditCategories` and `internal/audit/lambda_collect.go`.
- **Console links** (AXE-050) extend the existing `case "lambda"` in
  `internal/consolelink` with `layer` and `event-source-mapping` types.
- **EOL table** — the runtime-deprecation dates live in one place,
  `internal/expiry/eol.go`, now exposed via `expiry.LambdaRuntimeDeprecation`
  so both `expiring` and the new findings linter share the single source of
  truth (and under-warn together).

### New / changed shared pieces

| Piece | Introduced by | Purpose |
|-------|---------------|---------|
| `internal/lambdatui` | AXE-046/047/048 | Bubble Tea dashboard, modelled on `gluetui` |
| `internal/findings/lambda.go` | AXE-049 | `LambdaSnapshot` + `AnalyzeLambda` pure checks |
| `internal/audit/lambda_collect.go` | AXE-049 | best-effort `ListFunctions` collection for the audit |
| `expiry.LambdaRuntimeDeprecation` | AXE-049 | exported accessor for the shared runtime-EOL table |
| `consolelink` `layer` / `event-source-mapping` | AXE-050 | type-specific Lambda console URLs |

---

## Theme K — AWS Lambda / serverless compute

### AXE-045 — Lambda collector {#axe-045}

> **Status: ✅ pre-existing.** `internal/services/lambda/collector.go` already
> lists functions (`ListFunctions`) with bounded-concurrency per-function tag
> enrichment, mapping each to a `model.Resource`. The dashboard does not depend
> on it (it has its own region-fanned client), but it keeps functions in
> `summary` / `find` / `whereused` / snapshot diffs.

### AXE-046 — `lambda` interactive dashboard TUI {#axe-046}

> **Status: ✅ shipped** — `aws_explorer lambda` (`cmd/lambda.go`,
> `internal/lambdatui`). A tabbed Bubble Tea dashboard over **Functions**,
> **Layers** and **Event sources**, with `/` filter, `S`/`R` sort, `o` console,
> `r` refresh and `i` about. Per-region/per-listing failures degrade softly.

**UX.**

```bash
aws_explorer lambda                       # dashboard, all configured regions
aws_explorer lambda --region us-east-1    # pin one region
aws_explorer lambda --all-regions         # sweep + Region column
```

Opens on **Functions**. State is shown with the shared glyph vocabulary
(`✓` active/enabled, `●` transitional, `✗` failed, `○` inactive/disabled).
Tab switching preserves each pane's selection; the column sort resets per tab
because each tab has its own columns.

**Keys.** `tab`/`shift+tab` panes · `↑/↓` rows · `Enter` detail · `f` findings ·
`L` logs (Functions) · `S`/`R` sort/reverse · `/` filter · `o` console ·
`r` refresh · `i` about · `q`/`esc` back/quit. All registered through
`internal/ui` so the status bar stays truthful.

**Acceptance criteria.**
- Launch with zero args renders the Functions pane from the configured regions;
  a per-region load failure logs a note rather than crashing.
- Every pane is filterable and sortable; `o` yields a valid console URL.
- Tab switching preserves each pane's selection.

### AXE-047 — Function configuration detail overlay {#axe-047}

> **Status: ✅ shipped** — `Enter` on a function fetches `GetFunction` on demand
> and opens a **full-screen, btop-style grid of per-section panels** (mirroring
> the EMR describe view, `internal/lambdatui/detail_panels.go`): Overview,
> Resources & limits, State & health, VPC networking, Environment, Layers, Code
> package and Tags — each an independently scrollable tile. `Tab`/arrows move
> focus between tiles; the focused tile scrolls; the grid collapses to one
> scrolling pane on a short terminal so the status bar is never clipped (rule
> #9). Layers and event-source mappings reuse the same grid, built synchronously
> from the loaded inventory. Section building is pure and fixture-tested
> (`detail_test.go`).

**Privacy.** Environment-variable *values* are never collected onto the model or
rendered — only the keys (and a count) — so a secret passed as an env var can't
leak onto the screen or into a screenshot. This is the Lambda analogue of the
Glue/Glue-arg redaction rule.

**Acceptance criteria.**
- Each concept (VPC, environment, code/repository, tags…) is its own panel;
  absent fields show "—" and empty panels say so explicitly.
- Env-var values never appear; reserved concurrency distinguishes "unreserved"
  (no reservation) from a numeric reservation (including the throttled `0`).
- The grid reflows to 1/2/3 columns by width and falls back to a single
  scrolling pane when too short, never clipping the status bar.

**Debug pane.** The dashboard also embeds the shared `~` debug overlay
(`internal/debugpane`) that every other TUI has — a live view of the scan's
activity log — which the first cut omitted, so `~` did nothing while the
inventory loaded. It now opens over any view (including mid-load) and falls
through non-key messages so the load keeps progressing (rule #10).

### AXE-048 — Jump from a function to its CloudWatch logs {#axe-048}

> **Status: ✅ shipped** — `L` on a function suspends the dashboard and runs
> `aws_explorer cw --group /aws/lambda/<name>` as a child (the `tea.ExecProcess`
> pattern), inheriting `--region`/`--profile`/`--config`; `esc`/`q` returns. The
> group uses the function's explicit `LoggingConfig.LogGroup` when set, else the
> conventional `/aws/lambda/<name>`. Arg construction is pure and table-tested
> (`jump_test.go`).

### AXE-049 — `lambda` audit category {#axe-049}

> **Status: ✅ shipped** — `aws_explorer audit --only lambda`
> (`internal/findings/lambda.go` + `internal/audit/lambda_collect.go`), wired
> into the category list so `--fail-on`/`--ignore`/SARIF all apply. The same
> `AnalyzeLambda` powers the dashboard's `f` findings panel.

**Checks** (each a pure function over a `LambdaSnapshot`; stable check-IDs, with
positive+negative fixtures in `lambda_test.go`):

| ID | Finding | Detection | Severity |
|----|---------|-----------|----------|
| `LAM-RUN-001` | Deprecated runtime | runtime's EOL date is in the past | 🟡 |
| `LAM-RUN-002` | Runtime deprecating soon | EOL date within 90 days | 🔵 |
| `LAM-CFG-001` | No dead-letter queue | `DeadLetterConfig` absent | 🔵 |
| `LAM-CFG-002` | Failed state | `State == Failed` or `LastUpdateStatus == Failed` | 🟡 |

**Under-warn discipline.** Runtime checks read the shared EOL table
(`expiry.LambdaRuntimeDeprecation`); a runtime missing from it doesn't fire.
Container-image functions carry no runtime identifier and are skipped. The
health check fires only when the list response actually reported a state
(`StateKnown`), so a sparse response silences it rather than guessing. The DLQ
check is informational and worded to state what is known (no DLQ) without
asserting events are definitely dropped — an on-failure destination, which
`ListFunctions` does not expose, is a valid alternative.

### AXE-050 — CLI twins + console deep links {#axe-050}

> **Status: ✅ shipped** — `lambda functions|layers|event-sources`, each
> honouring `-o table|json|ndjson|csv` and `--region`/`--all-regions`. `lambda`
> with no subcommand launches the dashboard. Render helpers are fixture-tested
> (`render_test.go`). Console links extend the `case "lambda"` switch with
> `layer` (`#/layers/<name>`) and `event-source-mapping` (function list)
> URLs; functions keep their existing `#/functions/<name>` URL.

---

## Cross-cutting requirements

1. **Permissions documented.** The README's Lambda section and `docs/lambda.md`
   list the exact `lambda:*` (and `sts:GetCallerIdentity`, `logs:*` for the
   jump) actions; any denial degrades that feature with a visible note.
2. **Read-only guarantee.** Nothing here mutates Lambda. A future "invoke now"
   would follow the cost-stating confirmation pattern and is out of scope.
3. **Testing.** Every mapping/analysis/render/arg function is pure and
   fixture-tested; the AWS client wrappers stay thin.
4. **Key bindings.** New TUI keys reuse the shared `internal/ui` hints so the
   status bar stays truthful; checked for per-screen collisions.
5. **Docs.** A README feature section, `docs/lambda.md` guide, and this design
   doc; per-command reference pages auto-generate from the cobra tree.
