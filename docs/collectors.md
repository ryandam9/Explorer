# Collector coverage matrix

AWS Explorer has **typed collectors** for the services below. A typed collector
issues real `List*`/`Describe*` calls, so it surfaces resources **even when they
are untagged** — unlike the broad Resource Groups Tagging API discovery sweep,
which only returns *tagged* resources.

Two things this table makes explicit:

- **"29 services" is not "every resource type in those services."** Each
  collector covers the resource *types* listed here; the **Known gaps** column
  names the notable types it does not yet collect.
- Services **not** in this table can still appear in `summary`/`find` output via
  the tagging-API discovery sweep, but only if their resources are tagged.

> The **Service** column is verified against the engine's registered collectors
> by a test (`TestCollectorMatrixMatchesRegistry`), so this list cannot silently
> drift out of sync with the code.

| Service | Scope | Resource types collected | Detailed mode | Primary IAM | Known gaps |
|---|---|---|:---:|---|---|
| `acm` | regional | `certificate` | – | `acm:ListCertificates` | expiry, validation, SANs (no `DescribeCertificate`) |
| `apigateway` | regional | `restApi`, `httpApi`, `websocketApi` | – | `apigateway:GET` | stages, routes, integrations, authorizers, domains |
| `athena` | regional | `workGroup` | – | `athena:ListWorkGroups` | data catalogs, named queries, result config |
| `cloudformation` | regional | `stack` | – | `cloudformation:DescribeStacks` | drift, outputs/params, termination protection |
| `cloudfront` | global | `distribution` | – | `cloudfront:ListDistributions` | origins, WAF, viewer cert, logging |
| `cloudwatch` | regional | `alarm` | yes | `cloudwatch:DescribeAlarms` | composite alarms; log groups (see the `cw` command) |
| `dynamodb` | regional | `table` | yes | `dynamodb:ListTables`, `DescribeTable` | GSIs/LSIs, streams, PITR, TTL, SSE |
| `ec2` | regional | `instance`, `vpc`, `subnet`, `security-group`, `volume`, `network-interface` | yes | `ec2:Describe*` | route tables, IGW/NAT, endpoints, EIPs, snapshots, AMIs (some in the `vpc` TUI) |
| `ecr` | regional | `repository` | – | `ecr:DescribeRepositories` | image scan/lifecycle/replication config |
| `ecs` | regional | `cluster` | yes | `ecs:ListClusters`, `DescribeClusters` | services, tasks, task definitions, capacity providers |
| `efs` | regional | `fileSystem` | – | `elasticfilesystem:DescribeFileSystems` | mount targets, encryption, lifecycle policy |
| `eks` | regional | `cluster` | yes | `eks:ListClusters`, `DescribeCluster` | node groups, Fargate profiles, add-ons |
| `elasticache` | regional | `cacheCluster` | – | `elasticache:DescribeCacheClusters` | replication groups, serverless, encryption |
| `elbv2` | regional | `loadbalancer` | – | `elasticloadbalancing:Describe*` | target groups, listeners, target health |
| `emr` | regional | `cluster`, `step` | yes | `elasticmapreduce:ListClusters`, `DescribeCluster` | instance groups/fleets |
| `eventbridge` | regional | `eventBus`, `rule` | – | `events:ListEventBuses`, `ListRules` | rule targets, EventBridge Scheduler |
| `glue` | regional | `database`, `job`, `crawler`, `trigger`, `workflow`, `connection` | yes | `glue:GetDatabases`, `ListJobs`, … | tables, classifiers, security configs |
| `iam` | global | `role`, `user`, `group`, `policy`, `instance-profile` | yes | `iam:ListRoles`/`ListUsers`/`ListGroups`/`ListPolicies`/`ListInstanceProfiles` | OIDC/SAML providers, server certs, access keys |
| `kinesis` | regional | `stream` | – | `kinesis:ListStreams` | shards, retention, encryption, consumers |
| `kms` | regional | `key` | – | `kms:ListKeys`, `DescribeKey`, `ListAliases` | rotation status, key policy |
| `lambda` | regional | `function` | yes | `lambda:ListFunctions`, `ListTags` | event source mappings, function URLs, VPC config, layers |
| `rds` | regional | `instance`, `cluster` | yes | `rds:DescribeDBInstances`, `DescribeDBClusters` | snapshots, proxies, parameter/subnet groups |
| `redshift` | regional | `cluster` | – | `redshift:DescribeClusters` | serverless, encryption, network config |
| `route53` | global | `hostedZone` | – | `route53:ListHostedZones` | records, health checks, resolver rules |
| `s3` | global | `bucket` | yes | `s3:ListAllMyBuckets` (+ per-bucket GETs in detailed mode) | detail fields are fetched only in detailed mode |
| `secretsmanager` | regional | `secret` | – | `secretsmanager:ListSecrets` | last-accessed date, replication status |
| `sns` | regional | `topic` | – | `sns:ListTopics` | attributes, subscriptions, encryption |
| `sqs` | regional | `queue` | – | `sqs:ListQueues` | attributes, DLQ config, message counts |
| `stepfunctions` | regional | `stateMachine` | – | `states:ListStateMachines` | status, logging, definition, recent executions |

## Notes

- **Scope** — `global` collectors run once (against `us-east-1` for endpoint
  signing) and stamp resources with region `global`; `regional` collectors run
  per in-scope region (see `--region` / `--all-regions`).
- **Detailed mode** — a `yes` means the collector adds extra fields under
  detailed/raw detail levels. Summary output stays compact regardless.
- **Best-effort** — within a multi-family collector (e.g. `ec2`, `rds`, `iam`),
  a denied or failed call for one family degrades only that family; the rest are
  still collected and the failure is reported as a partial result.
- **Known gaps** are tracked collectively in the collector-completeness issue.
