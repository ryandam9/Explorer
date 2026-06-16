# Trail Usage

`trail` is a CloudTrail activity feed. It answers both *who changed this
resource, and when?* and *what has been happening in this account?* For each
event it prints when, which API call, which principal (short form —
`role/deploy-pipeline`, `user/alice`, `root`), from which source IP, and
whether the call failed. Events are newest first.

Scope is **one** filter at a time (LookupEvents accepts a single lookup
attribute): a resource, `--by`, `--event`, `--source`, or nothing for the
account-wide feed.

```bash
# Who touched this security group?  (resource-scoped)
aws_explorer trail sg-0abc123

# What has been happening in the account in the last 2 hours?  (account feed)
aws_explorer trail --since 2h

# Everything a principal did
aws_explorer trail --by alice

# Every instance-termination call, in a specific region
aws_explorer trail --event TerminateInstances -r eu-west-1

# Failed / denied calls only — recon & misconfiguration triage
aws_explorer trail --errors-only --since 24h

# ARNs work too — reduced to the resource name CloudTrail records.
aws_explorer trail arn:aws:iam::123456789012:role/app -r us-east-1

# Machine-readable
aws_explorer trail my-bucket -o json | jq '.[0]'
```

```
SNO  TIME                 EVENT                          PRINCIPAL             SOURCE IP     OUTCOME
1    2026-06-11 14:02:11  AuthorizeSecurityGroupIngress  role/deploy-pipeline  203.0.113.7   ok
2    2026-06-09 09:15:42  RunInstances                   user/alice            198.51.100.2  AccessDenied
```

| Flag | Default | Description |
|------|---------|-------------|
| `--by` | — | Only events by this principal (IAM user or role session name) |
| `--event` | — | Only this API call (e.g. `TerminateInstances`) |
| `--source` | — | Only events from this service (e.g. `ec2.amazonaws.com`) |
| `--errors-only` | off | Only failed/denied calls (events carrying an `errorCode`) |
| `--since` | full window | Only events after this long ago (`7d`, `36h`, or a plain day count) |
| `--limit` | `50` | Maximum number of events to print (`--tui` defaults to 200) |
| `--read-events` | off | Include read-only (`Describe*`/`List*`/`Get*`) events, marked `(read)` in the table |
| `--tui` | off | Explore the feed interactively (filter, sort, failed-only toggle, per-event detail) |

Notes:

- Uses `cloudtrail:LookupEvents`, which covers the **last 90 days** of
  management events with **no trail or S3 bucket setup required** — that one
  permission is all it needs. (Distinct from the `audit --only cloudtrail`
  category, which inspects trail *configuration*.)
- A resource, `--by`, `--event` and `--source` are **mutually exclusive** —
  the API matches a single attribute per query.
- CloudTrail records events in the region where the activity happened; pick it
  with `-r` (default: the first configured region). **`--all-regions`** fans the
  lookup out across regions (queried in parallel) and merges them newest-first.
  Global services such as IAM and CloudFront record in `us-east-1`.
- By default only **mutating** events are shown — the `Describe*` noise would
  drown out the changes you're looking for.
- The API is rate-limited (2 TPS); pages are fetched serially and capped. The
  account-wide feed scans deeper than a pivoted lookup, but on a busy account
  its newest events can still be all read-only — if you see nothing, **pivot**
  (`--event`/`--source`/`--by`) so CloudTrail filters server-side, add
  `--read-events`, narrow with `--since`, or use **`lake`** for older history.
  The tool says when a result was truncated and suggests these levers.

Add `--tui` to explore the feed interactively: quick filter, column sort, a
**failed-only toggle** (`x`), and a per-event detail overlay — the same
interaction language as the other TUIs. The scope (resource / `--by` /
`--event` / `--source` / account-wide) and region are set by the flags above;
the TUI then makes that feed navigable.

The resource-scoped view also lives in the summary TUI: press **`t`** on a
resource's detail panel for its CloudTrail timeline (failed calls flagged in
red).


# Lake Usage

`lake` queries a **CloudTrail Lake event data store** with SQL. Where `trail`
uses `cloudtrail:LookupEvents` (90 days, management events only), a Lake store
can hold **years** of history and **data events** (S3 object access, Lambda
invokes, …) and supports **aggregation** — at the cost of having to create a
store first.

```bash
# What stores can I query?
aws_explorer lake --list-stores

# Recent activity in the last 30 days
aws_explorer lake --since 30d

# Who has been the busiest principal this quarter?
aws_explorer lake --top-principals --since 90d

# Most frequent API calls, explored interactively
aws_explorer lake --top-events --tui

# Your own SQL (you supply the FROM clause / event-data-store id)
aws_explorer lake --sql "SELECT eventName, COUNT(*) c FROM <eds-id> GROUP BY eventName ORDER BY c DESC LIMIT 20"
```

```
NAME         ID         ARN
audit-store  abcd-1234  arn:aws:cloudtrail:us-east-1:123456789012:eventdatastore/abcd-1234
```

| Flag | Default | Description |
|------|---------|-------------|
| `--list-stores` | off | List available event data stores and exit |
| `--store` | the only store | Event data store to query (id, ARN, or name) |
| `--top-principals` | off | Built-in query: principals ranked by event count |
| `--top-events` | off | Built-in query: API calls ranked by frequency |
| `--sql` | — | Raw CloudTrail Lake SQL (you supply the `FROM`) |
| `--by` / `--event` / `--source` | — | Narrow the built-in queries (principal substring / API / service) |
| `--errors-only` | off | Narrow the built-in queries to failed/denied calls |
| `--since` | full store | Only events after this long ago (`30d`, `12h`) |
| `--limit` | `50` | Maximum number of rows |
| `--max-wait` | `60s` | How long to wait for the query to finish |
| `--tui` | off | Explore the results interactively (filter, numeric-aware sort, detail, CSV) |

Notes:

- Needs `cloudtrail:{ListEventDataStores,StartQuery,GetQueryResults}` (and
  `DescribeQuery` for failure detail). The query runs server-side; the tool
  polls `GetQueryResults` until it finishes or `--max-wait` elapses.
- **If no event data store exists**, the command prints a short note and exits
  cleanly — use `aws_explorer trail` for the zero-setup 90-day feed.
- CloudTrail Lake is regional; a multi-region store is queried from its home
  region. Pick the region with `-r`.
