## Context

Issue #143. Today an Approval must come from someone other than the plan author (ADR 0002 §4, `core.Derive` skips the author's `approved`, `core.CheckApprove` refuses it) and `approval.required_approvals` must be at least 1 (`internal/config`). A developer alone can never reach `approved`.

Decisions from the Grill (see the Messages on `intent.md`):

- `required_approvals: 0` means **self-approval**: no Approval by another person is needed, but the plan still needs one Approval through the `approve` Gate, and the author's own counts. `bfc submit` approves nothing. An agent never approves (ADR 0011).
- 0 is allowed only in config version 4; in versions 1 to 3 it is an error that names version 4. `bfc init` writes version 4.
- The `submitted` Event writes `required_approvals: 0` explicitly. An older `bfc` reads it as 1 and skips the author's Approval; accepted and documented, no data format bump.
- `request-changes` stays for people other than the author.

## Approach

- `core.Event` gets `SelfApproval bool` (submitted only): encoded as an explicit `required_approvals: 0`, parsed from an explicit 0; a missing count still means 1.
- `core.Status` gets `SelfApproval bool` from the `submitted` of the open round; `RequiredApprovals` stays `max(n, 1)`, so it reads "0 of 1". In `derive`, an `approved` by the author is valid when the round has self-approval (still not with `agent`). `CheckApprove` lets the author approve under self-approval.
- The Inbox, Prime "Waiting on you" and the TUI already go through `CheckApprove`, so they follow once the Index stores the flag (new column, bump `schemaVersion`).
- `bfc submit` sets `SelfApproval` when the config says 0.
- Config: `Version = 4`, the 0 rule, the comment in `Render`.

## Affected areas

`internal/config`, `internal/core` (`event.go`, `state.go`, `review.go`), `internal/index`, `cmd/bfc/review.go` (submit), `cmd/bfc/init.go` tests, docs: new `docs/adr/0016-self-approval.md`, ADR 0002 and 0003, `docs/design.md`, `CONTEXT.md`; `.beforecode/config.yaml` of this repository.

## Out of scope

A data format bump or `bfc migrate`; per-plan or per-person overrides; changing who may `request-changes` or `withdraw`.

## Tasks

- T-1: Config version 4: accept `required_approvals: 0` only in version 4, reject negative values, `Render` writes version 4 with an updated comment; table tests in `internal/config`.
- T-2: Core: `Event.SelfApproval` (explicit `required_approvals: 0`), `Status.SelfApproval`, the author's Approval in `derive` and `CheckApprove` under self-approval; table tests for the data format, state derivation, skipped events and the check.
- T-3: Wire it: `bfc submit` writes self-approval from the config, the Index stores the flag (schema bump); `cmd/bfc` tests in a temporary repository: author submits and approves, plan is `approved`, Inbox and Prime offer the approval; without 0 the author is still refused.
- T-4: Docs: ADR 0016, ADR 0002 and 0003 (version 4 section), `docs/design.md`, `CONTEXT.md`; set this repository's `.beforecode/config.yaml` to version 4 with `required_approvals: 0`.

## Acceptance criteria

- AC-1: `internal/config` accepts `required_approvals: 0` in a version 4 file, rejects it in versions 1 to 3 with an error naming version 4, rejects negative values, and `Render` writes version 4.
- AC-2: `bfc submit` with `required_approvals: 0` writes a `submitted` Event with `required_approvals: 0`; an Event without the field is still read as 1.
- AC-3: In a review round opened by such an Event, the plan author can `bfc approve` their own plan and it becomes `approved` (`in-progress` once a Task is claimed); without it the author's Approval is refused and skipped as today. An Approval with `agent` set is skipped in both cases.
- AC-4: The Inbox, Prime "Waiting on you" and the TUI offer the author the approve action on their own plan in review only under self-approval; the Index keeps the flag.
- AC-5: ADR 0016 records the decision; ADR 0002, ADR 0003 (version 4 section), `docs/design.md` and `CONTEXT.md` (Approval, Approval policy) follow it.
- AC-6: This repository's `.beforecode/config.yaml` is version 4 with `required_approvals: 0`.
