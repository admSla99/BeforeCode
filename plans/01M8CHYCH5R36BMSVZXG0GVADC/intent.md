---
title: 'Self-approval: required_approvals: 0 lets the plan author approve their own plan'
labels: [phase-3]
---

## Problem

A plan needs an Approval by someone other than its author, and `required_approvals` must be at least 1. A developer who works alone, or tests `bfc` alone while it is in development, can never get a plan to `approved`, so the edit hook blocks all work.

## Who it is for

A single developer using `bfc` on their own repository, and the maintainer testing `bfc` without a second person.

## How we know it worked

With `required_approvals: 0` in `.beforecode/config.yaml`, the plan author submits a plan, approves it with `bfc approve` (or in the TUI), and the plan is `approved`. With 1 or more, nothing changes.

## Out of scope

A data format bump or `bfc migrate`; per-plan or per-person overrides. Issue #143.
