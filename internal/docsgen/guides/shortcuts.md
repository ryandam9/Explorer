Every TUI shares the same interaction language: a context-aware status bar
that shows only the keys usable right now, horizontal column scrolling with
`<` / `>`, `/` to filter, `s`/`R` to sort, `C` to export CSV, `?` for help and
`q` to quit. This page collects the keys that are common across screens; the
per-screen guides list the rest.

## Universal keys (most TUIs)

| Key | Action |
|-----|--------|
| `↑` / `↓` / `j` / `k` | Navigate the focused list or table |
| `Enter` | Drill in / open the detail overlay |
| `Tab` / `Shift+Tab` | Move focus between panels |
| `<` / `>` (or `,` / `.`) | Scroll table columns when wider than the panel |
| `/` | Quick filter (live `matched/total` count) |
| `s` / `R` | Sort by the next column / reverse the direction |
| `y` | Copy the selection (ARN, ID, URI or line, per screen) |
| `o` | Open the selection in the AWS console (copies a deep link; opens a browser when local) |
| `C` | Export the current view to CSV under `~/.aws_explorer/exports/` |
| `Esc` | Close an overlay / go back one level |
| `?` | Help overlay |
| `q` / `Ctrl+C` | Quit |

## Screen-specific highlights

| Key | Screen | Action |
|-----|--------|--------|
| `Ctrl+P` | Summary TUI | Fuzzy-jump to any resource by name/ID/ARN |
| `D` | Summary TUI | Baseline / diff the inventory ("what changed") |
| `P` | Summary TUI | Switch profile/region and rescan |
| `F` | VPC explorer | Run the findings linter |
| `t` | VPC explorer | Connectivity path trace from an ENI |
| `P` | VPC explorer | Public-exposure audit |
| `A` | VPC explorer | AWS Reachability Analyzer |
| `D` | S3 browser | Download the selected object |
| `g` | S3 browser | Generate a presigned URL |
| `x` | S3 browser | Delete (only with `--allow-delete`) |
| `&` | CloudWatch viewer | Grep filter the log (regex) |
| `f` | CloudWatch viewer | Toggle live follow / tailing |
| `x` | Bill TUI | Per-resource cost breakdown |
| `u` | Bill TUI | Refresh now (paid request) |

See each TUI's own guide for the complete list:
[Summary](guide-summary.md) · [VPC explorer](guide-vpc.md) ·
[S3 browser](guide-s3.md) · [CloudWatch Logs](guide-cloudwatch.md) ·
[Audit & Bill](guide-reports.md).
