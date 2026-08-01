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
| `--since` | `24h` | Event query window, e.g. `30m`, `2h`, `3d` |
| `--theme` | `spotted-pardalote` | UI theme name |

```bash
# Browse log groups in one region
./bin/aws_explorer cw --region us-east-1

# Open a group and search for errors
./bin/aws_explorer cw -g /aws/lambda/my-fn -f ERROR

# Only scan the last 30 minutes of events (faster on busy groups)
./bin/aws_explorer cw -g /aws/lambda/my-fn --since 30m
```

Press `o` on a log group to open it in the CloudWatch console (URL copied;
browser opened when the session is local).

### Events panel

Opening a stream (`Enter`) or searching a whole group (`G`) lists matching
events. The query runs server-side (`FilterLogEvents`) over a bounded
**query window** — narrower windows scan less data, so busy groups answer
faster. The active window shows in the panel header and the status bar.

| Key | Action |
|-----|--------|
| `/` | Set the server-side query pattern (CloudWatch filter syntax) |
| `p` | Cycle the query window: 30m → 1h → 3h → 6h → 12h → 24h → 3d → 7d |
| `t` | Toggle between the plain list and a zebra-striped table (Time / Stream / Message, the same table widget used across the app) |
| `←`/`→` | In table mode, pan long messages sideways in 40-char steps (a `msg panned +N chars` note tracks the offset; ellipses mark text hidden off either edge). Hidden columns are revealed first, and the time column stays pinned |
| `Enter` | Open the full log viewer for the selected event's target |
| `W` | Toggle live tail watch mode |
| `y` / `s` | Copy the selected event / export the listed events |

Table cells show a 160-character window of each message so the layout stays
stable; `←`/`→` slide that window across the full text, and the whole message
is always available via `Enter` (full log viewer) or `y` (copy). The `Stream`
column appears in group-level search (`G`), where events interleave from many
streams.

### Full log viewer

Pressing `Enter` on a log event opens the **full log viewer**: a full-screen
page with the entire log (the selected query window, most recent 2000 events)
for the selected stream — or the whole group in group-level search — that
streams new events live as they arrive. Each line is tinted by severity (error/fail/panic
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
