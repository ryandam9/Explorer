---
name: Bug report
about: Report something that's broken or behaving unexpectedly
title: ""
labels: bug
assignees: ""
---

## What happened

A clear description of the bug.

## Steps to reproduce

1. Command / TUI used (e.g. `aws_explorer s3 --bucket my-bucket`)
2. ...
3. What you saw vs. what you expected

## Environment

- `aws_explorer --version`:
- OS / arch:
- AWS auth method (sso / profile / env / static / sts) and region(s):

## Relevant output

<details>
<summary>Logs / screenshot</summary>

```
paste here (redact any account IDs / ARNs / secrets you don't want public)
```

</details>

## Notes

This is a read-only tool by design — if the bug involves an unexpected
mutating/paid AWS call, please call that out explicitly.
