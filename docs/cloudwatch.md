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
| `--max-events` | `0` | Event ceiling for the full log viewer. `0` uses `cw.maxEvents` from the config, falling back to `50000`; a negative value removes the ceiling entirely |
| `--theme` | `spotted-pardalote` | UI theme name |

```bash
# Browse log groups in one region
./bin/aws_explorer cw --region us-east-1

# Open a group and search for errors
./bin/aws_explorer cw -g /aws/lambda/my-fn -f ERROR

# Only scan the last 30 minutes of events (faster on busy groups)
./bin/aws_explorer cw -g /aws/lambda/my-fn --since 30m

# Hold every event in the window in the log viewer, with no ceiling
./bin/aws_explorer cw -g /aws/lambda/my-fn --max-events -1
```

Press `o` on a log group to open it in the CloudWatch console (URL copied;
browser opened when the session is local). Press `?` anywhere — including
inside the full log viewer — for the full key reference; the status bar only
shows the keys usable right now (eliding on narrow terminals).

### Map of the UI

Every surface has a fixed name (shown in its heading), so docs, the `?` help
overlay, and conversations can refer to them unambiguously. The numbers match
the Tab-cycle order.

| Name | What it is |
|------|------------|
| **Browser** | The main screen: the sidebar plus one right-hand panel |
| **[1] Log groups** | Sidebar listing groups across the region scope |
| **[2] Log streams** | Right panel: the selected group's streams — or, after `F`, the streams that contain a given string |
| **[3] Log events** | Right panel: events for the selected stream — or the whole group after `G` (the heading shows which) |
| **Log viewer** | Full-screen page (`Enter` on an event): live tail, find/grep, table mode |
| **Event record** | Overlay (`v`): one event, every field unclipped |

### Filters & search — which one when

There are five narrowing tools, one per layer. `C` clears them all at once.

| Key | Where | What it narrows | Runs |
|-----|-------|-----------------|------|
| `/` | Group sidebar | The **list of group names** shown (also matches region) | Client-side, cosmetic |
| `/` | Streams panel | The **list of stream names** shown | Client-side, cosmetic |
| `F` | Streams panel | Not a filter — **a search**: which streams contain a string. One group-wide query, tallied per stream (matches, first/last match), so you find the right stream before opening any events. `Enter` on a result opens that stream with the pattern applied | **Server-side** |
| `/` | Events panel | **Which events AWS returns** — one or more [CloudWatch filter patterns](https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/FilterAndPatternSyntax.html) separated by `;` (e.g. `ERROR; timeout; { $.level = "error" }`). Each pattern runs as its own query and an event matching **any** of them is included, deduplicated into one timeline. Narrower patterns scan less data, so busy groups answer faster | **Server-side** |
| `G` | Group sidebar | Not a filter — **scope**: search events across the whole group (all streams interleaved) instead of one stream, with the same pattern and window. With no pattern set, `G` opens the pattern prompt first — `Enter` on the empty prompt explicitly browses everything, `Esc` backs out | Server-side |
| `/` | Full log viewer | Nothing — **find-in-page**: highlights matches, `n`/`N` jump between them, every line stays visible | In-page |
| `&` | Full log viewer | The **lines rendered** — only lines matching the regex show (like `less`), with a kept/total count | In-page |

Rules of thumb: use the list `/`s to *find* a group or stream by name; use `F`
when you know the string but not the stream — it names the streams instead of
dumping their events; use the events-panel `/` (plus the `p` window) when the
log is busy and you only want certain events fetched at all; use `G` when you
want the matching events themselves, interleaved across streams; and inside the viewer use `/` to *locate* something while
keeping context, or `&` to *isolate* matching lines.

Applied filters stay visible: each panel shows its active filter value, the
status bar switches to `shown/total` counts while a list is filtered, and
`C` resets everything (in the viewer, `C` clears find and grep).

### Finding the right stream (`F`)

When you know the string but not which stream it landed in, `F` on the **[2]
Log streams** panel answers that directly — without opening a single event.

It runs **one** `FilterLogEvents` query over the whole group (the same call `G`
makes) and tallies the result by stream, because AWS returns the stream name on
every matched event. There is no per-stream fan-out, so the cost is one group
query regardless of how many streams the group has.

| Column | Meaning |
|--------|---------|
| `Stream` | The log stream that contained a match |
| `Matches` | How many events matched in the window (`≥N` when the scan was capped) |
| `First match` / `Last match` | When the first and last match landed |

Streams are ordered by last match, so whatever is active right now is on top.

| Key | Action |
|-----|--------|
| `F` | Open the prompt (seeded with the active event pattern, if any); `F` again edits it |
| `Enter` | In the prompt: run the search. On a result: open that stream's events **with the pattern already applied** |
| `↑`/`↓` | Move between matched streams |
| `Esc` | Cancel the prompt / clear the results and return to the plain stream list |

Three things the view is careful about:

- **It answers for the query window only** — the header says which (`p` changes
  it). "No stream matched in the last 24h" is not "this string appears
  nowhere"; widen the window and ask again.
- **Only matching streams are listed**, and the header counts them against the
  group's total (`12 of 40 streams matched`), so "no match" never looks like
  "not queried".
- **A capped scan is labelled.** Counting reads up to 50,000 matched events; past
  that the counts become minimums (shown `≥N`), a warning appears, and a very
  quiet stream may be missing entirely — narrow the pattern or the window for an
  exact answer.

### [3] Log events panel

Opening a stream (`Enter`) or searching a whole group (`G`) lists matching
events. The query runs server-side (`FilterLogEvents`) over a bounded
**query window** — narrower windows scan less data, so busy groups answer
faster. The active window shows in the panel header and the status bar.

| Key | Action |
|-----|--------|
| `/` | Set the server-side query pattern(s). Separate several with `;` to OR them — `ERROR; timeout` shows events matching either, across every stream when combined with `G`. Each pattern runs as its own `FilterLogEvents` query; results are deduplicated and interleaved by time. The pattern(s) also scope the full log viewer and the `D` download, so "download only the matched lines across all streams" is: `G` → set patterns → `D` |
| `p` | Cycle the query window: 30m → 1h → 3h → 6h → 12h → 24h → 3d → 7d |
| `J` | In table view, expand JSON embedded in each message into indented lines **inside the Message column** — the same thing `J` does to the viewer's log lines, so the key means one thing everywhere. Expanded cells get a more generous line cap (40) than the default 8, since asking to expand is asking to read it |
| `t` | Toggle between the plain list and a zebra-striped table. The table is **Time and Message, nothing else** — the stream and each JSON field's full value are in the record view (`v`), so the width goes to the message. The Message column takes whatever Time leaves, so the table fills the terminal, and a message too long for it wraps onto continuation rows aligned under the column rather than being cut off. A wrapped event still selects, stripes and navigates as one row; past 8 lines the last line says how many were left |
| `Enter` | Open the full log viewer for the selected event's target |
| `v` | Record view: the selected event vertically, with every JSON field's **full value** (table cells clip at 80/160 chars; this is the escape hatch). Scrollable, `y` copies the record, `Esc` closes |
| `W` | Toggle live tail watch mode |
| `y` / `s` | Copy the selected event / export the listed events |
| `D` | Download **every** matching event in the query window to the downloads directory — `s` writes only the events currently listed (~100), while `D` re-queries the window in full (up to 50,000 events; the toast notes when that cap truncates). Also works from the group sidebar (whole group) and the streams panel (selected stream); the active query pattern and window apply |

`J` never adds columns: it changes the shape of the Message cell, and the
expanded document wraps and aligns under the column like any other long
message. The choice is shared with the viewer's line view, so switching
between `t` and the log lines keeps it.

The `Stream` column is gone from the table: in a whole-group search (`G`) the
stream of the selected event is named in the record view (`v`), which also
shows every JSON field's full value. That keeps the table's width for the
message, which is what you are usually reading.

### Log viewer

Pressing `Enter` on a log event opens the **Log viewer**: a full-screen
page with the entire log for the selected stream (or the whole group in
group-level search), streaming new events live as they arrive. The initial
load pages the *whole* selected query window — every event in it, not just the
most recent few — so `p` (the query window) is the lever that decides how much
history you see. Only very large logs are capped, at 50,000 events with the
newest kept; when that happens the header and status bar say `truncated`
rather than passing a partial log off as the complete one. The ceiling is
yours to set — `--max-events`, or `cw.maxEvents` in the config, with a negative
value removing it altogether (the query window then being the only bound). Each line is tinted
by severity (error/fail/panic in red, warnings amber, info/notice in the info
color, debug/trace muted) so errors stand out while you scroll.

| Key | Action |
|-----|--------|
| `↑`/`↓`, `PgUp`/`PgDn`, `Ctrl+U`/`Ctrl+D` | Scroll (scrolling up pauses tailing) |
| `g` / `G` | Jump to top / jump to bottom and resume tailing |
| `f` | Toggle follow (auto-scroll as new events stream in) |
| `t` | Toggle a table view of the streamed events — the same Time/Message table as the events panel, with record view (`v`) and per-row copy (`y`). Clear any grep filter first; follow (`f`/`G`) keeps the cursor on the newest row as events stream in |
| `J` | Toggle JSON formatting: pretty-prints JSON objects/arrays embedded in log messages (a `{} json` badge shows while on) |
| `/` | Search within the log (case-insensitive, matches highlighted; search works on the formatted lines when `J` is on) |
| `&` | Grep filter (as in `less`): enter a regex and only matching lines are rendered, with a `kept/total` count; `Enter` keeps the filter, `Esc` clears it. Invalid patterns are flagged while the last valid filter stays applied |
| `n` / `N` | Jump to next / previous match |
| `y` | Copy the entire log to the clipboard — or only the matching lines while a grep filter is applied |
| `s` | Export the log to the downloads directory (default `~/.aws_explorer/downloads`) — or only the matching lines (file suffixed `-grep`) while a filter is applied |
| `?` | Full key reference |
| `Esc` / `q` | Close the viewer |
