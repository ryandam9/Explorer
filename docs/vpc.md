# VPC Explorer TUI Usage

An interactive, three-pane TUI for drilling into a single VPC's networking and
attached resources. Pick a VPC on the left, a resource category in the middle,
and browse the matching resources on the right.

```bash
./bin/aws_explorer vpc [flags]
```

If `--region` is omitted, all regions are scanned for VPCs.

### VPC Flags

The global `--profile`, `--auth-method`, `--role-arn`, `--region` and
`--all-regions` flags apply. With no region flags, all regions are scanned.

| Flag | Default | Description |
|------|---------|-------------|
| `--theme` | `spotted-pardalote` | Color theme |

### Layout

```
┌─ VPCs ──────┬─ Resources ─────┬─ Subnets ─────────────────────────────┐
│ vpc-0a1b... │ ▸ NETWORK       │  #  Name   CIDR          AZ    Public  │
│ vpc-2c3d... │   Subnets       │  1  -      172.31.0.0/20 ...   Yes     │
│ my-vpc      │   Security Grps │  2  -      172.31.16.0/20 ...  Yes     │
│ default     │   Route Tables  │                                       │
│             │ ▸ COMPUTE       │                                       │
│             │   EC2 Instances │                                       │
│             │ ▸ SERVICES      │                                       │
└─────────────┴─────────────────┴───────────────────────────────────────┘
```

### Resource categories

The middle pane groups the resource types a VPC contains. Selecting one (with
`Enter`) loads it into the right-hand table.

- **NETWORK** — Subnets, Security Groups, **Network Interfaces** (ENIs), Route Tables, Internet Gateways, NAT Gateways, VPC Endpoints, Network ACLs, Peering, Flow Logs
- **COMPUTE** — EC2 Instances, Lambda Functions
- **SERVICES** — RDS Instances, Load Balancers

Each table shows a default set of columns; the full attribute set (plus tags,
rule lists, etc.) appears in the **detail overlay** when you press `Enter` on a
row. Which columns and detail fields are shown can be overridden per resource
type in `config.yaml` — see [Customizing displayed columns](configuration.md#customizing-displayed-columns).

### Keyboard shortcuts

**Navigation**

| Key | Action |
|-----|--------|
| `↑` / `↓` / `j` / `k` | Move within the VPC list, category sidebar, or resource table |
| `Enter` | Open a VPC · load a category · open the selected row's detail overlay |
| `Tab` | Switch focus between the category sidebar and the resource table |
| `<` / `>` (or `,` / `.`) | Scroll table columns left/right when a table is wider than the panel |
| `/` | Filter the VPC list by name or ID · quick-filter the resource table (matches any column, live `matched/total` count; `Enter` keeps the filter, `Esc` clears it) |
| `s` / `R` | Sort the resource table by the next column / reverse the direction |
| `c` / `y` | Copy the selected resource's ID to the clipboard |
| `o` | Open the selected resource in the AWS console — copies the deep-link URL, opens a browser when local |
| `C` | Export the current resource table to CSV under `~/.aws_explorer/exports/` |
| `r` | Refresh the VPC list or the current resource list |
| `Esc` | Go back one level (overlay → table → sidebar → VPC list) |
| `S` | Open the settings panel (themes & colors) |
| `i` | About this page — a short overlay explaining what the VPC Explorer does |
| `?` | Toggle the help overlay |
| `q` / `Ctrl+C` | Quit |

**Debugging toolkit** (available in the resource browser)

| Key | Action | Cost |
|-----|--------|------|
| `F` | **Findings** — run the VPC linter and list ranked issues | free |
| `t` | **Trace** — connectivity path tracer from the selected network interface | free |
| `x` | **Where used** — cross-reference the selected resource | free |
| `e` | **Effective rules** — merged security-group rules for the selected ENI | free |
| `D` | **DNS** — the VPC's DNS resolution / hostnames / DHCP options | free |
| `P` | **Public exposure** — everything reachable from the internet | free |
| `w` | **What changed** — baseline the VPC, then diff against it later | free |
| `E` | **Export** — write a Markdown report of resources + findings | free |
| `A` | **Reachability Analyzer** — list AWS Network Insights analyses; create new ones | listing free; creating ~$0.10/analysis |
| `L` | **Logs** — jump to the CloudWatch Logs explorer for the selected Lambda function or RDS instance | free |

Inside any overlay, `↑` / `↓` scroll and `Esc` (or the same trigger key) closes it.

### Horizontal column scrolling

Wide tables (e.g. Security Groups, with ~106 columns of data) don't truncate or
drop columns on narrow terminals. The leading identifier columns stay **pinned**
while the remaining columns scroll with `<` / `>`; a `◀ N more cols ▶` indicator
shows when columns are hidden off either edge, and the status bar advertises
`</>` only while there is something to scroll to. This works the same in every
table of the application — the summary TUI, the S3 browser and the VPC
explorer.

---


# VPC Debugging Toolkit

The VPC Explorer is built for the questions a cloud/support engineer actually
asks. Every analysis below is **deterministic** — computed locally from the
resources AWS returns, with no AI — and the one feature that can change anything
in AWS (`A`) is read-only by default and asks for confirmation before any paid
call. Most overlays fetch a one-shot *snapshot* of the VPC's networking
(subnets, security groups, ENIs, route tables, gateways, NACLs, peerings,
endpoints) and reason over it.

### Plain-English rule explanations

Opening the detail overlay (`Enter`) for a **Security Group** or **Network ACL**
adds an "In plain English" section that translates each rule into a readable
sentence:

```
  In plain English:
  • Allow inbound HTTPS (TCP 443) from anywhere on the internet (0.0.0.0/0)
  • Allow inbound SSH (TCP 22) from anywhere on the internet (0.0.0.0/0)  ⚠ remote admin access open to the entire internet
  • Allow inbound MySQL/Aurora (TCP 3306) from resources in security group sg-0abc123
```

- **Ports** are named from a table of ~60 well-known services (22→SSH, 443→HTTPS, 3306→MySQL/Aurora, 5432→PostgreSQL, 6379→Redis, 3389→RDP, …).
- **Sources/destinations** are classified: public (`0.0.0.0/0`), IPv6 (`::/0`), single host (`/32`), RFC1918 private networks, security-group references (`sg-…`), and prefix lists (`pl-…`).
- **Risk flags (`⚠`)** are added only for genuinely dangerous exposure to the public internet — remote-admin ports (SSH/RDP/VNC/Telnet), database/cache ports, all-ports/all-traffic, or a port range spanning sensitive ports. Ordinary public web ports (HTTP/HTTPS) are intentionally **not** flagged, to avoid alert fatigue.
- **NACL** explanations additionally show the rule number and allow/deny action, label the catch-all as `Rule * (default)`, and note that NACLs are **stateless** and evaluated in ascending rule-number order (first match wins).

### Findings linter (`F`)

Scans the whole VPC and opens a scrollable table of issues sorted most-severe
first — severity (`🔴 critical`, `🟡 warning`, `🔵 info`), the impacted
resource, the issue and why it fired, and the suggested fix:

```
VPC Findings — 1 critical, 2 warning, 0 info

SEVERITY     RESOURCE     ISSUE                                  FIX
─────────────────────────────────────────────────────────────────────────────────────
🔴 CRITICAL  sg-0a1       Security group exposes a sensitive     Restrict the source to
                          port to the internet                   specific CIDRs or a
                          sg-0a1 (default): Allow inbound SSH    security group instead
                          (TCP 22) from anywhere on the          of 0.0.0.0/0.
                          internet (0.0.0.0/0)
```

The checks:

| Area | Finding | Severity |
|------|---------|----------|
| Security groups | Sensitive port (admin/database/all) open **inbound** to `0.0.0.0/0` — ranges covering a sensitive port rank the same as the port itself | 🔴 critical |
| Security groups | Rule references a security group not in this VPC | 🔵 info |
| Route tables | Blackhole route (target deleted) | 🟡 warning |
| Subnets | Low available IPs / >90% utilization | 🟡 warning |
| Subnets | Auto-assign public IP but no IPv4 internet-gateway route | 🟡 warning |
| Subnets | No outbound internet path (no IGW/NAT/eigw/TGW/peering/NAT-instance default route) | 🔵 info |
| NAT gateways | Available but unreferenced by any route (idle, still billing) | 🟡 warning |
| Internet gateways | Detached from the VPC | 🔵 info |
| Network ACLs | Stateless return-traffic gap (ephemeral ports not allowed back) | 🟡 warning |
| Peering | Overlapping CIDRs (all CIDR blocks, including secondaries) · not active | 🟡 / 🔵 |
| VPC endpoints | Gateway endpoint with no route-table association | 🟡 warning |
| VPC endpoints | Interface endpoint SGs don't allow inbound 443 · private DNS off | 🟡 / 🔵 |
| **Capacity** | Rules per SG (limit 60), routes per route table (50), rules per NACL (20), SGs per ENI (5), subnets per VPC (200) | 🟡 ≥80%, 🔴 at limit |
| **Orphans** | Security group attached to nothing & unreferenced · empty subnet | 🔵 info |

The NACL stateless check evaluates rules in rule-number order with
first-match-wins (a broad deny shadows later allows, exactly like AWS), is
careful to *not* flag the correct "inbound 443 + outbound ephemeral" pattern,
and also covers the default NACL — its rules are editable, so a hardened
default NACL is linted like any other. Capacity limits are AWS defaults
(adjustable via Service Quotas; account-specific increases are not reflected).
Orphan checks are skipped if ENI data is unavailable.

### Connectivity path tracer (`t`)

The "can't connect" doctor. From a selected **Network Interface**, press `t` and
enter a destination as `IP[:port]` (or `internet:443`). It walks the path the
way AWS evaluates it and reports the **first hop that blocks** the connection:

```
❌ Blocked at: Destination security group ingress

• Source                              eni-web (10.0.0.10) in subnet subnet-pub
✓ Security group egress               sg-web allows all traffic
✓ Source NACL egress                  acl-default rule 100 allows it
✓ Route table                         10.0.0.0/16 → local (local)
✓ Destination NACL ingress            acl-default rule 100 allows it
✗ Destination security group ingress  no ingress rule on sg-db allows TCP 5432 from 10.0.0.10
```

It evaluates, in order: source security-group **egress** (stateful) → source
NACL **egress** (stateless, ordered, first-match-wins) → **route-table**
longest-prefix lookup (local / IGW / NAT / blackhole) → for in-VPC
destinations, the destination NACL **ingress** and security-group **ingress**
(resolving `sg-` references against the peer ENI) → and the **stateless return
path** (ephemeral ports 1024–65535). Internet via an internet gateway requires
the source to hold a public IP/EIP; via a NAT gateway it's treated as private
egress — and both internet paths also verify the source NACL lets the
**stateless replies** back in on ephemeral ports. A NAT gateway that is not in
the `available` state blocks the path. Traffic between two interfaces in the
**same subnet** correctly skips the NACL hops (NACLs apply only at the subnet
boundary), and destination IPs are matched against ENIs' **secondary private
IPs** as well as primaries.

Known limitations: IPv4 only (IPv6 routes and `::/0` rules are not evaluated),
and managed prefix lists (`pl-…`) in rules or routes cannot be expanded — the
trace flags a caveat when one is present, since the verdict may be incomplete.
Paths into peered VPCs or transit gateways are evaluated up to the gateway and
reported as "open up to" that target.

### Cross-reference — "where used" (`x`)

`x` shows everything that references the selected resource and what it
references, turning the flat tables into a navigable graph. It works on
**security groups, subnets, route tables, network interfaces, NAT gateways,
internet gateways, network ACLs, VPC endpoints, and peering connections** —
the `x` hint appears in the status bar only on those categories, and pressing
it elsewhere says so explicitly instead of showing an empty result:

```
Where used: subnet-priv
Route table  (1)                    • rtb-priv
Network ACL  (1)                    • acl-priv
Network interfaces in subnet  (1)   • eni-b
```

Covered: **security groups** (attached ENIs + their instances, and other SGs
referencing them), **subnets** (route table & NACL — including the implicit
main/default when unassociated — plus ENIs and NAT gateways), **route tables**
(associated subnets and non-local targets), **network interfaces**
(instance/subnet/SGs), **NAT & internet gateways** (route tables routing to
them), and **network ACLs** (associated subnets).

### Effective security rules (`e`)

An ENI can carry several security groups, and AWS evaluates the **union** of
their rules. On a Network Interface, `e` shows the merged, de-duplicated
inbound/outbound rules in plain English, annotated with the contributing
group(s):

```
Effective rules: eni-app
Security groups: sg-a, sg-b

Inbound  (3)
  • Allow inbound HTTPS (TCP 443) from anywhere on the internet (0.0.0.0/0)
      via sg-a, sg-b          ← identical rule in both groups, collapsed
Network ACL acl-1 also applies to this subnet (stateless, evaluated separately).
```

### DNS & VPC attributes (`D`)

For the "DNS doesn't resolve in my VPC" case. Shows the attributes plus
diagnostic notes:

```
DNS resolution        Enabled
DNS hostnames         Disabled
DHCP options set      dopt-abc
Domain name servers   10.0.0.2, 8.8.8.8
Domain name           corp.internal

Notes
  🟡 DNS hostnames disabled — interface VPC endpoints' private DNS will not resolve.
  • Custom DNS servers bypass the Amazon Route 53 Resolver; private hosted zones /
    endpoint private DNS may not resolve unless those servers forward to it.
```

`enableDnsSupport` off is flagged critical, `enableDnsHostnames` off is a
warning, and custom DHCP DNS servers are noted as info.

### Public exposure (`P`)

A one-screen audit of the VPC's internet-facing surface:

```
Public exposure — internet-facing surface
⚠ Internet-reachable interfaces (public IP + IGW route + open security group)
                                                                 (1)  • eni-pub (52.1.1.1) → i-web — HTTPS (TCP 443)
Public subnets (route to an internet gateway)                    (1)  • subnet-pub
Security groups open to the internet (inbound from 0.0.0.0/0)    (1)  • sg-web (web) — HTTPS (TCP 443)
Network interfaces with a public IP                              (1)  • eni-pub (52.1.1.1) → i-web
```

The first group **correlates** the three ingredients of real exposure — an ENI
holding a public IP, in a subnet routing to an internet gateway, with a
security group open to the internet — and lists the ports actually reachable,
so a permissive-but-unrouted security group doesn't read as an incident. The
remaining groups list each ingredient on its own: public subnets (IPv4 or IPv6
default route to an IGW), SGs with their internet-open ports in plain English
(SG-to-SG references excluded), and ENIs holding a public IP/EIP.

### Snapshot diff — "what changed" (`w`)

For "it worked yesterday". The first `w` on a VPC saves a baseline snapshot;
later, `w` diffs the live VPC against it and shows exactly what changed —
added/removed resources and, for resources that still exist, the specific facts
(rules, routes, attributes) that were added or removed:

```
Changes since baseline — 1 added, 1 removed, 1 modified
+ Security group sg-new
- Security group sg-old
~ Security group sg-web
    by role/deploy-pipeline — AuthorizeSecurityGroupIngress, 2026-06-11 14:02 UTC
    + inbound|tcp|22|10.0.0.0/8
```

Inside the overlay, **`t` attributes each change to its likely actor**: the
most recent CloudTrail mutation event for every changed resource (when, which
API call, which principal), via the zero-setup 90-day `LookupEvents` window —
the same source as `aws_explorer trail`. Lookups run serially (the API allows
2/s) and are capped at the first 15 changed resources; a denied
`cloudtrail:LookupEvents` degrades to a one-line note. The actor shown is the
*latest* to touch the resource — the likely, not guaranteed, author of the
diff.

Baselines are stored as JSON in `~/.aws_explorer/vpc-snapshots/<vpc-id>.json`.
Inside the overlay, `b` re-baselines to the current state. Volatile fields (like
available-IP counts) are deliberately excluded so they don't create noise.
Tracked facts include SG rules, routes and route-table associations, **NACL
rules and subnet associations**, subnet attributes, NAT gateway state/subnet/
**public IP**, IGW state, peering status, and endpoint state/private-DNS/route
tables/**security groups/subnets** — covering the classic silent breakers like
a NACL re-association or an endpoint SG swap.

### Export (`E`)

Writes a self-contained report — a resource-count summary, all findings grouped
by severity with fixes, and inventory tables (subnets, security groups, route
tables, NAT gateways, endpoints, network interfaces) — in three formats sharing
a basename under `~/.aws_explorer/exports/<vpc-id>-<timestamp>.{md,html,svg}`:

- **Markdown** (`.md`) — ideal for pasting into a support case or runbook.
- **HTML** (`.html`) — styled, with a sticky table-of-contents and searchable,
  paginated resource tables; leads with the architecture diagram and a checkbox
  bar to **toggle diagram layers** — Subnets, Traffic & IGW, NAT gateways,
  Security groups, Detail labels — implemented in pure CSS (`:has()`), so it
  works offline with no JavaScript.
- **SVG** (`.svg`) — a deterministic **architecture diagram**: the internet and
  its gateway, the VPC as a container, availability-zone columns of subnets
  (wrapping into lanes when an AZ has many) colour-coded public / private /
  isolated by their default route, NAT gateways drawn in their subnet,
  per-subnet security-group badges, and arrows for the traffic-flow paths
  (internet ⇄ IGW, public → IGW, private → NAT → IGW). Pure function of the
  snapshot — no AI.

The status bar shows the paths.

### AWS Reachability Analyzer (`A`)

Integrates the authoritative AWS [Reachability Analyzer](https://docs.aws.amazon.com/vpc/latest/reachability/what-is-reachability-analyzer.html).
**Read-only by default** — `A` lists the Network Insights analyses that already
exist in the account, each as `source → destination:port` with a
`reachable` / `not reachable` / `running` / `failed` verdict:

```
Reachability Analyzer
✓ eni-web → eni-db:3306 (tcp)  [reachable]       2026-06-09 10:00
✗ eni-web → igw-1 (tcp)  [not reachable]         2026-06-09 11:30
```

Creating a new analysis is **opt-in**: press `n`, enter
`source -> destination[:port]` (prefilled with the selected network interface),
then confirm a prompt that **states the cost** before anything is created:

```
⚠ This creates AWS resources and incurs a per-analysis charge (~$0.10).
  eni-web → eni-db:3306
y = create and run  •  n/Esc = cancel
```

On confirmation it creates a Network Insights path, starts the analysis, polls
until it completes (up to ~2 minutes), and prepends the result. This is the only
VPC Explorer feature that mutates AWS or incurs a charge.

> **Files written by the toolkit.** Snapshots: `~/.aws_explorer/vpc-snapshots/`.
> Exports: `~/.aws_explorer/exports/`. Both directories are created on demand.
> All other features are purely in-memory.
