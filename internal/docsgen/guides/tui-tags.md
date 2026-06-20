`aws_explorer tags` is an interactive explorer for finding resources **by tag**.
It answers questions like "what's tagged `Environment=prod`?" or "show me
everything owned by `Team=payments`" — by browsing the tags actually configured
in the account, or by typing a tag filter directly.

```bash
# Browse tags in the configured region
aws_explorer tags

# Sweep every enabled region
aws_explorer tags --all-regions

# Pin one region
aws_explorer tags --region ap-southeast-2
```

## What it shows

A **three-column layout** (Keys ▸ Values ▸ Resources), all visible at once:

1. **Keys** — every tag key in the account.
2. **Values** — the selected key's values.
3. **Resources** — every resource carrying the selected `Key=Value`.

Two ways to reach resources:

1. **Drill across** — move with `↑`/`↓` in the focused column and press `Enter`
   (or `→`) to drill into the next column to the right; `←` / `Esc` steps focus
   back, `Tab` cycles. Selecting a key loads its values; selecting a value lists
   the matching resources. Values and resources load **on demand** (on `Enter`),
   never while scrolling.
2. **Filter entry** — press `f` (or `/`) and type one or more `Key=Value`
   filters, comma-separated, to fill the Resources column directly.

Each key/value row shows a **Resources** count that fills in progressively in
the background (`…` while counting; `N+` if a region's count failed). Counts are
best-effort and follow the same coverage caveat below.

Filter syntax:

| You type | Meaning |
|---|---|
| `Environment=prod` | resources with that exact tag |
| `Env=prod, Team=payments` | **both** tags (AND across keys) |
| `Team=payments, Team=billing` | `Team` is `payments` **or** `billing` (OR within a key) |
| `Owner` | resources that have an `Owner` tag with any value |
| `Team=payments \|\| Env=prod` | either group (OR across keys, via `\|\|`) |
| `Env=prod, type:ec2:instance` | scope to a resource type |

The Tagging API matches only **tagged** resources, so there is no "untagged"
filter. The CLI twin takes the same syntax plus a `--type` flag:

```bash
aws_explorer tags resources --filter "Team=payments || Team=billing" -o csv
aws_explorer tags resources --filter Env=prod --type ec2:instance,s3:bucket -o json
```

## Coverage (important)

Data comes from the **Resource Groups Tagging API**, so the explorer shows
**only tagged resources on services that integrate with that API** — untagged
resources, and services it doesn't cover (IAM, for example), won't appear. The
UI states this so an empty result isn't mistaken for "nothing exists". Per-region
failures are flagged ("N region(s) failed … press r to retry") rather than shown
as "no results".

## Shortcuts

| Key | Action |
|-----|--------|
| `↑` / `↓` · `g` / `G` | Move within the focused column |
| `Enter` / `→` | Drill into the next column (keys → values → resources) |
| `←` / `Esc` | Step focus back one column |
| `Tab` / `Shift+Tab` | Cycle focus between columns |
| `f` / `/` | Type a `Key=Value` filter |
| `<` / `>` | Scroll the wide resources table sideways |
| `y` | Copy the selected resource's ARN |
| `o` | Open the selected resource in the AWS console |
| `r` | Refresh the focused column |
| `i` | About · `q` Quit |

Scope is the active region by default; add `--all-regions` to sweep every
enabled region (global resources such as CloudFront and Route 53 appear under
`us-east-1`).

## Scripting

The same data is available non-interactively for pipelines, with
`-o table|json|ndjson|csv`:

```bash
aws_explorer tags keys --all-regions -o json
aws_explorer tags values --key Environment -o csv
aws_explorer tags resources --filter "Environment=prod,Team=payments" -o ndjson
```

The coverage caveat and any per-region failures are written to stderr, so stdout
stays clean for scripts; CSV cells are sanitized against spreadsheet formula
injection.
