# ADR 0002: Data format v1 for the plans ref

Date: 2026-11-16. Status: accepted.

## Decision

This document specifies the files inside `refs/beforecode/plans` (see ADR 0001) for `format-version` 1. Storage code, state derivation and tests follow it exactly. Terms are those of `CONTEXT.md`.

The design rests on three rules:

1. Every Event and every Message is its own file named by a ULID and is never changed or deleted, so concurrent writers never conflict.
2. Plan state is derived from Events and the current `plan.md`; it is never stored.
3. An Approval names the exact version of `plan.md` it approves.

## 1. Layout

```
format-version
plans/
  <ULID>/
    intent.md
    plan.md
    events/<ULID>.yaml
    messages/<ULID>.md
    handoff/            reserved (phase 2)
    checks/             reserved (phase 3)
```

- **ULID.** 26 characters, Crockford base32, upper case, as in the ULID specification. A plan directory is named by the plan's ULID only, with no title slug, so the path never changes when the title does. The ULID of an Event or Message file is that item's ID.
- **`intent.md`** is written by `bfc new`. **`plan.md`** is created from the template the first time someone edits it with `bfc edit`. It can be missing in a `draft` plan.
- **Append-only.** Files in `events/` and `messages/` are never modified, renamed or deleted. Only `intent.md` and `plan.md` change in place.
- **Reserved names.** A v1 binary never writes `handoff/` or `checks/` and ignores them when reading. It also ignores any other unknown file or directory.
- **Encoding.** All files are UTF-8 with LF line endings. Markdown files with metadata start with YAML front matter between two `---` lines.

## 2. `format-version`

A file at the root of the ref containing the integer `1` followed by LF. `bfc init` writes it with the first commit to the ref.

**Compatibility rule.** A binary supports exactly one format version, N.

- Ref version greater than N: the binary reads on a best-effort basis, shows a warning, and **refuses to write**.
- Ref version lower than N: the binary refuses to write until an explicit migration has run. A binary never upgrades or downgrades the format as a side effect of another write.
- Missing file on a ref that contains plans: an error reported by `bfc doctor`.

**When to bump the version.**

- No bump: adding an optional field. Readers ignore fields they do not know.
- Bump: adding an Event type, adding a directory or file that affects plan state, or changing the meaning or required presence of an existing field.

## 3. Common fields

| Field | Type | Meaning |
| --- | --- | --- |
| `author` | map `{name, email}` | The person whose `bfc` wrote the item, from `user.name` and `user.email` in git config. People are compared by `email`, case-insensitively. |
| `at` | string, RFC 3339 in UTC (`2026-11-16T21:30:00Z`) | Wall-clock time of writing, for display. **Ordering always uses the ULID**, never `at`. |
| `agent` | string, optional | Present when an AI wrote the text through a harness, for example `claude-code`. `author` is still the person who ran it. |

The author is stored in the file and not taken from the commit. A sync merge commit has its own author, and the CI bot writes on behalf of the workflow.

## 4. Events

File `plans/<plan ULID>/events/<event ULID>.yaml`. The common fields come first, then the fields of the event type at the top level:

```yaml
type: approved
plan: 01JD3K8Q2W5V7X9Y0Z1A2B3C4D
author:
  name: Jana Nováková
  email: jana@example.com
at: 2026-11-16T21:30:00Z
plan_blob: 3b18e512dba79e4c8300dd08aeb37f8e728b8dad
```

`plan` must equal the plan directory name. `plan_blob` is the git object ID of `plan.md` as stored in the ref, in the object format of the repository.

| `type` | Extra fields | Who may write it | Transition |
| --- | --- | --- | --- |
| `created` | `short_id` | anyone; this person becomes the **plan author** | none → `draft` |
| `submitted` | `plan_blob`, `required_approvals` (integer ≥ 1, from the approval policy at submit time, default 1) | plan author | `draft` → `in-review` |
| `withdrawn` | none | plan author | `in-review` or `approved` → `draft` |
| `approved` | `plan_blob` | anyone but the plan author, and not with `agent` | `in-review` → `approved` once enough valid Approvals exist |
| `changes-requested` | `plan_blob`, `message` (ULID of the Message with the reasons) | anyone but the plan author | `in-review` or `approved` → `draft` |
| `closed` | `message` (optional) | anyone | any state → `closed`, which is final |
| `claimed`, `checked`, `done` | reserved for phase 3 | | |

`bfc` refuses to write an event that is not allowed. It also refuses `approved` while the plan has an open Question (see section 7). That check runs only at write time; state derivation does not read Messages.

### State derivation

Input: the plan's Events sorted by ULID, and the object ID of the current `plan.md`.

1. The first `created` event defines the plan author and the Short ID. With no valid `created`, the plan is invalid and is reported by `bfc doctor`.
2. Walk the events in ULID order and keep the **review round**, which is the events after the last valid `submitted`.
   - An event is skipped if it is not allowed for its author (table above), if its `plan` field does not match, or if it does not fit the state at that point. For example, `submitted` is skipped outside `draft`, and `approved` is skipped outside a review round.
   - After `closed`, the state is `closed` and every later event is skipped.
3. **Ending a round.** A valid `withdrawn` or `changes-requested` ends the review round and the state becomes `draft`. An `approved` event that comes later in ULID order is outside any round and is skipped. As a result, the state is `draft` no matter in which order a concurrent `approved` and `changes-requested` arrive, so an approval cannot hide a reviewer's objection.
4. Within an open review round:
   - **Valid Approvals.** These are the `approved` events in the round whose `plan_blob` equals the current `plan.md`, counted once per approver email.
   - If their number is at least `required_approvals` of the round's `submitted`, the state is `approved`. Otherwise it is `in-review`.
   - Editing `plan.md` after approval therefore returns the plan to `in-review` without any new event. The author does not submit again; reviewers approve the new version.
5. Skipped events stay in the ref. `bfc doctor` lists them.

## 5. `intent.md`

```markdown
---
title: Payments via Stripe
labels: [payments, backend]
---

## Problem
## Who it is for
## How we know it worked
## Out of scope
```

- **`title`**: required, one non-empty line. It is the plan's title everywhere.
- **`labels`**: optional list of lower-case words made of `a-z`, `0-9` and `-`.
- The four sections are expected but not enforced. The Intent is a free-form human document.

## 6. `plan.md`

There is no front matter. Title and labels live only in `intent.md`.

`bfc submit` requires these level-2 headings (matched without regard to case; extra headings and any order are allowed):

`## Context`, `## Approach`, `## Affected areas`, `## Out of scope`, `## Tasks`, `## Acceptance criteria`

**Acceptance criteria.**

- Each Acceptance criterion is a top-level list item in the `## Acceptance criteria` section, in the form `- AC-<n>: <text>`. `<n>` is a positive integer without leading zeros, and `<text>` is non-empty.
- Continuation lines are indented by at least two spaces.
- Items have no checkboxes. Whether a criterion is met is reported by the Plan check.

```markdown
## Acceptance criteria

- AC-1: With an invalid card the API returns 402 and the order stays in state awaiting-payment.
- AC-2: A successful payment sends exactly one confirmation email,
  even when the webhook is delivered twice.
```

**Rules for IDs and submission.**

- IDs are unique within the file and are **never reused or renumbered**. A removed AC-3 leaves a gap, because Messages and Plan check results refer to criteria by ID.
- `bfc submit` requires at least one Acceptance criterion.
- `## Tasks` is a free-form list in v1. Its structure is specified in phase 3.

## 7. Messages

File `plans/<plan ULID>/messages/<message ULID>.md`. It has YAML front matter and a Markdown body.

```markdown
---
author:
  name: Peter Horváth
  email: peter@example.com
at: 2026-11-16T21:42:00Z
to: [jana@example.com]
ref: plan.md#AC-2
---

Does "exactly one" also apply when the customer retries the payment?
```

| Field | Required | Meaning |
| --- | --- | --- |
| `author`, `at` | yes | See section 3. |
| `agent` | no | Set when an AI wrote the text. |
| `to` | no | A list of email addresses and/or the word `ai`. A Message with `to` is a **Question**. |
| `reply_to` | no | ULID of the Message this one replies to, in the same plan. |
| `ref` | no | The part of the plan the Message is about: `intent.md`, `plan.md`, `plan.md#<heading anchor>` or `plan.md#AC-<n>`. A heading anchor is the GitHub-style slug of the heading text, for example `#affected-areas`. |

- **Thread.** A Message without `reply_to`, together with every Message that replies to it, directly or through other replies.
- **Answered Question.** A Question is answered when a direct reply exists whose author email is in `to`. For `ai`, a direct reply with `agent` set answers it. Other replies are discussion and leave the Question open.
- **References that go stale.** A `ref` whose heading or criterion no longer exists stays in the Message unchanged. The TUI shows it as stale.

## 8. Short ID

- **Shape.** The Short ID of a plan is `bfc-` followed by the first *n* characters of the random part of its ULID (characters 11 to 26), in lower case. For example, ULID `01JD3K8Q2W5V7X9Y0Z1A2B3C4D` has the random part `5v7x9y0z1a2b3c4d`, so its Short ID is `bfc-5v7x`.
- **Length.** *n* starts at 4. At `bfc new`, *n* grows by one while any plan already in the ref has the same first *n* random characters, up to a maximum of 16.
- **Fixed for life.** The chosen ID is stored as `short_id` in the `created` event and never changes. References such as `Plan: bfc-5v7x` in old pull requests stay valid.
- **Lookup.** `bfc` accepts the full ULID, `bfc-<prefix>`, or a bare prefix of at least four characters. The prefix must match the random part of exactly one plan; otherwise `bfc` lists the candidates. A query equal to a plan's stored Short ID wins first, so `bfc-5v7x` still names the plan that got it even after another plan got `bfc-5v7x9` (added in #10).
- **Concurrent collisions.** Two plans created offline at the same time can get the same Short ID. Both stay valid, a longer prefix tells them apart, and `bfc doctor` reports the duplicate.

## 9. Templates

`.beforecode/templates/intent.md` and `.beforecode/templates/plan.md` live on normal branches and are changed through pull requests. `bfc new` and `bfc edit` copy them and fill in `title`. They follow sections 5 and 6. The `plan.md` template contains an empty `- AC-1:` placeholder, which fails the submit check until it is filled in.

## Considered options

- **Title slug in the plan directory name** (as first sketched in `docs/design.md`). Rejected because the path would lie after a rename, and renaming the directory would break history and references.
- **Answers written into the question's file, or one discussion file per plan.** Rejected because two people answering at the same time edit the same lines, and the TUI syncs every 10 to 30 seconds. One file per Message with `reply_to` keeps the "never conflict" property. `bfc show` and `bfc export` still present the discussion as one document.
- **A `plan-edited` event to invalidate approvals.** Rejected because edits made outside `bfc` would be missed. Comparing `plan_blob` with the current `plan.md` cannot be forgotten.
- **Short ID from the start of the ULID.** Rejected because the first ten characters are a timestamp, so plans created close together would collide.
- **Approval count read from config at read time.** Rejected because changing the policy would silently change the state of existing plans. The count is fixed in `submitted`.
- **Last event wins between `approved` and `changes-requested`.** Rejected because which one counts would then depend on clock timing.

## Consequences

- Readers must ignore unknown fields and files. Writers must refuse to write any format version other than their own.
- State derivation needs the object ID of `plan.md`, which the store already has from the tree, so no file content is read.
- A plan is never physically deleted. `closed` hides it from the default views.
