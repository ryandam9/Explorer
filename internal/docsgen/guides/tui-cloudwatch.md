`aws_explorer cw` is an interactive explorer for CloudWatch log groups,
streams and events, with filtering, search and live tailing. `--region` pins
one region; `--all-regions` sweeps every enabled region and adds a Region
column to the group list.

```bash
# Browse log groups in one region
aws_explorer cw --region us-east-1

# Open a group and search for errors
aws_explorer cw -g /aws/lambda/my-fn -f ERROR
```

See the [`cw` command reference](cw.md) for every flag.

## Group / stream / event navigation

| Key | Action |
|-----|--------|
| `↑` / `↓` | Move through groups, streams or events |
| `Enter` | Select a group → its streams → a stream's events → the full log viewer |
| `/` | Filter the current list |
| `G` | Search the **entire group** (across streams) |
| `W` | Tail-watch the event list |
| `o` | Open the selected log group in the CloudWatch console |
| `y` / `s` | Copy / export events |
| `Tab` | Switch panel · `Esc` Back · `q` Quit |

## Full log viewer

Pressing `Enter` on an event opens the full-screen log viewer: the entire log
(24-hour lookback, most recent 2000 events) for the selected stream — or the
whole group in group-level search — streaming new events live as they arrive.

| Key | Action |
|-----|--------|
| `↑`/`↓`, `PgUp`/`PgDn`, `Ctrl+U`/`Ctrl+D` | Scroll (scrolling up pauses tailing) |
| `g` / `G` | Jump to top / bottom and resume tailing |
| `f` | Toggle follow (auto-scroll as new events stream in) |
| `J` | Toggle JSON formatting — pretty-prints embedded JSON (a `{} json` badge shows while on) |
| `/` | Search within the log (case-insensitive, matches highlighted) |
| `&` | Grep filter (as in `less`): a regex keeps only matching lines, with a `kept/total` count. Smart case: all-lowercase matches case-insensitively; an uppercase letter makes it exact |
| `n` / `N` | Jump to next / previous match |
| `y` | Copy the whole log — or only the matching lines while a grep filter is applied |
| `s` | Export the log to `~/.aws_explorer/logs/` (suffixed `-grep` while filtered) |
| `Esc` / `q` | Close the viewer |
