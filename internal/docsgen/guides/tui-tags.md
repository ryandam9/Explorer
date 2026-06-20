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

Two ways to reach resources:

1. **Drill down** — start on the list of **tag keys** in the account, press
   `Enter` to see that key's **values**, then `Enter` on a value to list every
   resource carrying that tag.
2. **Filter entry** — press `f` (or `/`) and type one or more `Key=Value`
   filters, comma-separated, to jump straight to matching resources.

Filter syntax:

| You type | Meaning |
|---|---|
| `Environment=prod` | resources with that exact tag |
| `Env=prod, Team=payments` | **both** tags (AND across keys) |
| `Team=payments, Team=billing` | `Team` is `payments` **or** `billing` (OR within a key) |
| `Owner` | resources that have an `Owner` tag with any value |

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
| `↑` / `↓` · `g` / `G` | Navigate the list |
| `Enter` | Drill in (key → values → resources) |
| `f` / `/` | Type a `Key=Value` filter |
| `←` / `→` | Scroll wide resource tables |
| `y` | Copy the selected resource's ARN |
| `o` | Open the selected resource in the AWS console |
| `r` | Refresh the current view |
| `Esc` | Back up one level |
| `i` | About · `q` Quit |

Scope is the active region by default; add `--all-regions` to sweep every
enabled region (global resources such as CloudFront and Route 53 appear under
`us-east-1`).
