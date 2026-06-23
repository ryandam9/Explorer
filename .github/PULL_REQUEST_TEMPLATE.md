## Summary

What this PR changes and why. Link any issues (e.g. `Closes #123`).

## Changes

-

## Testing

- [ ] `make fmt vet test` is green
- [ ] Added/updated tests for the behavior change
- [ ] Updated docs (`README.md` / `docs/`) if behavior changed

## Checklist (see CONTRIBUTING.md / CLAUDE.md)

- [ ] SDK pointer derefs are nil-guarded; `List*` calls paginate
- [ ] Errors aren't swallowed into defaults — "not set" vs "denied" vs "failed"
      are distinct and surfaced
- [ ] Stays **read-only by default**; any mutating/paid/outside-AWS action is
      opt-in with a cost-stating confirmation
- [ ] New TUI keys registered in `internal/ui`; layout measured (no clipped
      status bar)
