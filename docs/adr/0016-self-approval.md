# ADR 0016: Self-approval with `required_approvals: 0`

Date: 2026-11-26. Status: accepted. Decided by the maintainer for issue #143. Changes the approval rule of ADR 0002 section 4 and the config range of ADR 0003.

## Context

An Approval had to come from someone other than the plan author, and `approval.required_approvals` had to be at least 1. A developer who works alone, or who tests `bfc` alone while it is in development, could never get a plan to `approved`, and the edit hook (ADR 0011) then blocks all work. `bfc` should be usable by a single person.

## Decision

- **Meaning.** `approval.required_approvals: 0` is **self-approval**: no Approval by another person is needed, but the plan still needs one Approval, and the plan author's own Approval counts. The author takes the `approve` Gate like any reviewer (`bfc approve`, or the TUI). `bfc submit` approves nothing by itself, so the decision to start work stays a separate, deliberate step by a person.
- **Agents.** An agent never approves, under self-approval too (ADR 0011): an `approved` Event with `agent` is skipped and the Gate refuses to run inside an agent.
- **Other rules stay.** `request-changes` is still for people other than the author; the author takes a plan back with `withdraw`. An Approval still names the `plan_blob` it approves, so an edit makes it stale.
- **Config.** 0 is allowed only from config version 4. In versions 1 to 3 it is an error that names version 4, so an older `bfc` meeting a version 4 file says "update bfc" instead of rejecting the value. Negative values are errors. `bfc init` writes version 4; its default stays 1.
- **Data.** The `submitted` Event records the policy as an explicit `required_approvals: 0`. A missing field still means 1 (ADR 0002 section 4). In code, `core.Event.SelfApproval` marks the explicit 0, and `core.Status.SelfApproval` is set while the open review round was submitted with it; `Status.RequiredApprovals` is then 1, so the plan reads "0 of 1 approvals" until someone approves. Like the count, the policy is fixed at submit: changing the config does not change a plan already in review.
- **Where it shows.** The Inbox, Prime ("Waiting on you") and the TUI offer the author the approve action on their own plan only under self-approval, because they all ask `core.CheckApprove`. The Index stores the flag (schema version 5).

## Considered options

- **Approve automatically at submit.** Rejected: it removes the deliberate Gate between writing a plan and letting agents implement it, and the author could not look at the final `plan.md` once more before approving.
- **A separate key, such as `approval.allow_self_approval: true`.** Rejected: `required_approvals` already counts "Approvals by people other than the plan author", and 0 of them is exactly the solo case. One key cannot contradict another.
- **A data format bump and `bfc migrate`.** Rejected as too heavy for one optional field. See the consequence below.

## Consequences

- An older `bfc` reads `required_approvals: 0` as 1 and skips the author's Approval, so in a clone with an older binary such a plan stays `in-review`. Self-approval needs this version of `bfc` in every clone that reads the plans ref; with one user that is one binary. An older binary also stops on a version 4 config with "update bfc".
- `bfc doctor` does not list the author's Approval as a skipped Event when the round has self-approval.
- A team that turns self-approval on gives up the four-eyes rule for every plan submitted while it is on. The config is changed through a pull request, which the team reviews.
