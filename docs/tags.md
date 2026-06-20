# Tags explorer

`tags` opens an interactive explorer for finding AWS resources **by tag** —
"what's tagged `Environment=prod`?", "show me everything owned by
`Team=payments`". Browse the tags actually configured in the account, or type a
tag filter directly.

```bash
./bin/aws_explorer tags [--region us-east-1 | --all-regions] [--theme <name>]
```

## Two ways to find resources

1. **Drill down** — start on the account's **tag keys**, press `Enter` for a
   key's **values**, then `Enter` on a value to list every resource carrying
   that tag.
2. **Filter entry** — press `f` (or `/`) and type one or more `Key=Value`
   filters, comma-separated.

Filter syntax:

| You type | Meaning |
|---|---|
| `Environment=prod` | resources with that exact tag |
| `Env=prod, Team=payments` | **both** tags (AND across keys) |
| `Team=payments, Team=billing` | `Team` is `payments` **or** `billing` (OR within a key) |
| `Owner` | resources that have an `Owner` tag with any value |

## Coverage

Backed by the **Resource Groups Tagging API**, so it shows **only tagged
resources on services that integrate with that API** — untagged resources and
unsupported services (e.g. IAM) won't appear. The UI states this, and per-region
failures are flagged rather than shown as "no results". Use `--all-regions` to
sweep every region (global resources such as CloudFront/Route 53 appear under
`us-east-1`).

## Shortcuts

| Key | Action |
|-----|--------|
| `↑`/`↓` · `g`/`G` | Navigate |
| `Enter` | Drill in (key → values → resources) |
| `f` / `/` | Type a `Key=Value` filter |
| `←`/`→` | Scroll wide resource tables |
| `y` | Copy the selected resource's ARN |
| `o` | Open in the AWS console |
| `r` | Refresh · `Esc` Back · `i` About · `q` Quit |

## Required IAM

`tag:GetResources`, `tag:GetTagKeys`, `tag:GetTagValues` (all free, read-only).

## Related

- [Find / whereused](find.md) — fuzzy search by name/ARN and blast-radius lookups.
- [Summary](summary.md) — account-wide inventory (typed collectors + tag sweep).
