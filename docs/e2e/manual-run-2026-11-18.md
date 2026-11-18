
## Manual run on GitHub, 2026-11-18 13:32 CET

- Repository: https://github.com/admSla99/BeforeCode.git, ref `refs/beforecode/plans`
- Ana: admSla99 <188631245+admSla99@users.noreply.github.com>; Ben: Ben Bitdiddle <ben@example.com>; two clones on one machine (Linux)
- bfc dev; git version 2.43.0

### Prerequisites
- [x] bfc is on PATH
- [x] git is 2.38 or newer
- [x] gh is logged in as admSla99
- [x] main has .beforecode/config.yaml
- [x] bfc has the close command (#49)

### Setup
- [x] Ana's clone is on main and clean
- [x] Ben's clone is on main and clean

### Ana sets up BeforeCode
- Ana ran bfc init on a repository without refs/beforecode/plans.
- [x] Ana has the plans ref
- [x] Ana's checkout is untouched

<details><summary>Ana: bfc doctor before the first push</summary>

```
ok    git version: git version 2.43.0 (needs 2.38)
ok    repository: /home/sla2mlv/bfc-e2e-github/ana
ok    config: .beforecode/config.yaml is valid; syncs with origin
ok    plans ref: refs/beforecode/plans at ecff902
ok    fetch refspec: +refs/beforecode/*:refs/beforecode-remote/* for origin
FAIL  remote: origin has no refs/beforecode/plans yet
      fix: bfc push
ok    objects: all present
skip  in sync: needs refs/beforecode/plans here and on origin
ok    format-version: 1
ok    index: not built yet; the next read builds it
ok    event files: 0 valid
ok    plans: 0 valid
ok    skipped events: none
ok    short IDs: unique
Error: 1 check(s) failed
```

</details>


### Ana writes and submits
- [x] bfc-an5j is in review for Ana
- [x] GitHub has Ana's plans ref

### Ben joins
- [x] Ben has the remote's plans ref
- [x] Ben sees bfc-an5j in review
- [x] Ben sees the acceptance criterion
- [x] Ben's checkout is untouched

### Ben approves
- [x] Ben sees bfc-an5j approved
- [x] GitHub has Ben's approval

### Ana sees the approval
- [x] Ana sees bfc-an5j approved
- [x] approved by Ben

<details><summary>Ana: bfc show bfc-an5j</summary>

```
bfc-an5j  E2E 20261118-1332: payments via Stripe
ID:         01M7QADV1KAN5JV2A7XE0YRDZT
State:      approved
Author:     admSla99 <188631245+admSla99@users.noreply.github.com>
Approvals:  1/1 (ben@example.com)
Activity:   2026-11-18 13:39

--- intent.md ---
---
title: 'E2E 20261118-1332: payments via Stripe'
labels: []
---

## Problem

testing sentence

## Who it is for

For me dev

## How we know it worked

everything shold work

## Out of scope

nothing

--- plan.md ---
## Context
Card payments fail for some EU customers.
## Approach
Move checkout to Stripe Payment Intents.
## Affected areas
The checkout service.
## Out of scope
Refunds.
## Tasks
- Replace the charge call.
## Acceptance criteria
- AC-1: A card that needs 3-D Secure completes checkout.
```

</details>


### Concurrent writes
- [x] Ben's push said 'after merging changes pushed meanwhile' (seen by hand)
- [x] Ana and GitHub agree
- [x] Ben and GitHub agree
- [x] Ben has Ana's intent edit
- [x] Ana has Ben's new plan

<details><summary>Ben: bfc list</summary>

```
bfc-xjfk  draft     E2E 20261118-1332: search by customer email  ben@example.com                              2026-11-18 13:40
bfc-an5j  approved  E2E 20261118-1332: payments via Stripe       188631245+admSla99@users.noreply.github.com  2026-11-18 13:40
```

</details>


### Checkouts
- [x] Ana's checkout is untouched
- [x] Ben's checkout is untouched

### Clean up
- [x] bfc-an5j is closed
- [x] bfc-xjfk is closed
- [x] Ben and GitHub agree

<details><summary>Ben: bfc doctor</summary>

```
ok    git version: git version 2.43.0 (needs 2.38)
ok    repository: /home/sla2mlv/bfc-e2e-github/ben
ok    config: .beforecode/config.yaml is valid; syncs with origin
ok    plans ref: refs/beforecode/plans at d9d9c7c
ok    fetch refspec: +refs/beforecode/*:refs/beforecode-remote/* for origin
ok    remote: origin has refs/beforecode/plans at d9d9c7c
ok    objects: all present
ok    in sync: up to date with origin
ok    format-version: 1
ok    index: readable and current
ok    event files: 6 valid
ok    plans: 2 valid
ok    skipped events: none
ok    short IDs: unique
```

</details>


**Result: 29 checks passed, 0 failed.**
