`aws_explorer vpc` opens a three-pane explorer for a single VPC's networking
and attached resources: pick a VPC on the left, a resource category in the
middle, and browse the matching resources on the right. With no `--region`,
all regions are scanned for VPCs.

## Layout

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

## Resource categories

The middle pane groups the resource types a VPC contains. Select one with
`Enter` to load it into the right-hand table; `Enter` on a row opens a detail
overlay with the full attribute set.

- **NETWORK** — Subnets, Security Groups, Network Interfaces (ENIs), Route Tables, Internet Gateways, NAT Gateways, VPC Endpoints, Network ACLs, Peering, Flow Logs
- **COMPUTE** — EC2 Instances, Lambda Functions
- **SERVICES** — RDS Instances, Load Balancers

## Navigation shortcuts

| Key | Action |
|-----|--------|
| `↑` / `↓` / `j` / `k` | Move within the VPC list, category sidebar, or resource table |
| `Enter` | Open a VPC · load a category · open the selected row's detail overlay |
| `Tab` | Switch focus between the category sidebar and the resource table |
| `<` / `>` (or `,` / `.`) | Scroll table columns when wider than the panel |
| `/` | Filter the VPC list, or quick-filter the resource table (live `matched/total`) |
| `s` / `R` | Sort the resource table by the next column / reverse |
| `c` / `y` | Copy the selected resource's ID |
| `o` | Open the selected resource in the AWS console |
| `C` | Export the current resource table to CSV |
| `r` | Refresh the VPC list or the current resource list |
| `Esc` | Go back one level (overlay → table → sidebar → VPC list) |
| `S` | Settings panel (themes & colors) |
| `?` | Toggle the help overlay |
| `q` / `Ctrl+C` | Quit |

## Debugging toolkit

Available in the resource browser — a deterministic (no-AI) toolkit for
answering "why can't this connect?" and "what's exposed?":

| Key | Action | Cost |
|-----|--------|------|
| `F` | **Findings** — run the VPC linter and list ranked issues | free |
| `t` | **Trace** — connectivity path tracer from the selected ENI | free |
| `x` | **Where used** — cross-reference the selected resource | free |
| `e` | **Effective rules** — merged security-group rules for the selected ENI | free |
| `D` | **DNS** — the VPC's DNS resolution / hostnames / DHCP options | free |
| `P` | **Public exposure** — everything reachable from the internet | free |
| `w` | **What changed** — baseline the VPC, then diff against it later | free |
| `E` | **Export** — write a Markdown report of resources + findings | free |
| `A` | **Reachability Analyzer** — list AWS Network Insights analyses; create new ones | listing free; creating ~$0.10/analysis |

Inside any overlay, `↑` / `↓` scroll and `Esc` (or the same trigger key) closes
it. Wide tables (Security Groups carry ~100 columns) scroll horizontally with
`<` / `>` rather than truncating.
