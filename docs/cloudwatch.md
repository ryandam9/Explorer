# CloudWatch Logs TUI Usage

An interactive explorer for CloudWatch log groups, streams and events, with
filtering, search and live tailing.

```bash
./bin/aws_explorer cw [flags]
```

The global `--profile`, `--auth-method`, `--role-arn`, `--region` and
`--all-regions` flags apply: `--region` pins a single region, `--all-regions`
sweeps every enabled region and adds a Region column to the group list, and
otherwise the config's `aws.regions` list is used.

| Flag | Default | Description |
|------|---------|-------------|
| `--group` / `-g` | — | Initial log group filter/pattern |
| `--stream` / `-s` | — | Initial log stream filter |
| `--filter` / `-f` | — | Initial query pattern for log events |
| `--theme` | `spotted-pardalote` | UI theme name |

```bash
# Browse log groups in one region
./bin/aws_explorer cw --region us-east-1

# Open a group and search for errors
./bin/aws_explorer cw -g /aws/lambda/my-fn -f ERROR
```

Press `o` on a log group to open it in the CloudWatch console (URL copied;
browser opened when the session is local).

### Full log viewer

Pressing `Enter` on a log event opens the **full log viewer**: a full-screen
page with the entire log (24-hour lookback, most recent 2000 events) for the
selected stream — or the whole group in group-level search — that streams new
events live as they arrive. Each line is tinted by severity (error/fail/panic
in red, warnings amber, info/notice in the info color, debug/trace muted) so
errors stand out while you scroll.

| Key | Action |
|-----|--------|
| `↑`/`↓`, `PgUp`/`PgDn`, `Ctrl+U`/`Ctrl+D` | Scroll (scrolling up pauses tailing) |
| `g` / `G` | Jump to top / jump to bottom and resume tailing |
| `f` | Toggle follow (auto-scroll as new events stream in) |
| `J` | Toggle JSON formatting: pretty-prints JSON objects/arrays embedded in log messages (a `{} json` badge shows while on) |
| `/` | Search within the log (case-insensitive, matches highlighted; search works on the formatted lines when `J` is on) |
| `&` | Grep filter (as in `less`): enter a regex and only matching lines are rendered, with a `kept/total` count; `Enter` keeps the filter, `Esc` clears it. Invalid patterns are flagged while the last valid filter stays applied |
| `n` / `N` | Jump to next / previous match |
| `y` | Copy the entire log to the clipboard — or only the matching lines while a grep filter is applied |
| `s` | Export the log to `~/.aws_explorer/logs/` — or only the matching lines (file suffixed `-grep`) while a filter is applied |
| `Esc` / `q` | Close the viewer |
