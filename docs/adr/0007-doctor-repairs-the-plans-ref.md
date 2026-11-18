# ADR 0007: `bfc doctor --fix` repairs a damaged plans ref

Date: 2026-11-18. Status: accepted. Settled with the maintainer for issue #48. Extends ADR 0006.

## Decision

`bfc doctor --fix` repairs two kinds of damage to the plans ref. It leaves two others to the user, and says why.

| Damage | Check | `--fix` |
| --- | --- | --- |
| no `format-version` | format-version | adds it in a new commit, when every event type is known |
| objects missing from this clone | objects (new) | fetches them again from the sync remote; the rest are lost |
| an event file that breaks ADR 0002 | event files | no repair |
| a plan without a valid `created` event | plans | no repair; `bfc new` |

**A missing `format-version` is added** in a new commit on top of the tip, with the message `bfc doctor: add format-version 1`, through the same `Store.Write` as every other write. It is added also when the ref holds no plans, as after an interrupted `bfc init`. Format version 1 is the only one that has existed, so a ref without the file was written by a v1 binary or damaged by hand. A later binary must not guess, though: when an event file has a type this bfc does not know (`core.KnownEventType`; the reserved `claimed`, `checked` and `done` count as unknown), the check stays `FAIL`, lists those files and tells the user to update bfc. Only event files that parse are asked: one that does not parse is reported by the event-files check and does not stop the repair, because a broken event file is the common damage and must not block the fix.

**Missing objects are fetched again.** The new `objects` check, after `remote`, walks the tree of the tip commit and lists every tree and blob that is not in the object store, with its path. Only the tip counts, because state and documents come from it; missing objects of older commits are `git fsck`'s business. A plain `git fetch` does not bring an object back while the local ref already points to a commit that holds it: git negotiates and sends nothing. A fetch by object ID works only when the server allows it. So `--fix` runs `git fetch --refetch` (git 2.36, below our 2.38 minimum) with the plans refspec, which sends every object of the remote's plans ref without negotiation. The plans ref is small. The fetch moves only `refs/beforecode-remote/plans`, so no commit is made. Objects still missing afterwards, and every missing object in local mode or without a usable remote, are **lost**. The check names each one and says how to copy it from a clone that still has it (`git cat-file -p` there, `git hash-object -w --stdin` here).

**Every repair that changes the ref is a new commit** on top of the tip. No commit is rewritten, nothing is pushed, and the working tree, the index and the current branch are not touched. Fetching objects again changes no ref of this clone except the remote-tracking one, so it makes no commit.

**Broken event files are only reported.** Events are append-only (ADR 0002). An event that parses still counts, even when it breaks the format, and one that does not parse is ignored already. Marking an event as ignored inside the ref (a tombstone) would change plan state, which is a format change and needs format version 2 (ADR 0002 section 2). An ignore list outside the ref would make clones disagree about plan state.

**A plan without a valid `created` event is only reported.** A new `created` event would get a new ULID. `core.Derive` starts at the first `created` event, so every earlier event of the plan would be skipped, and the person who ran `bfc doctor` would become the plan author. A `created` event with a backdated ULID would keep the history but lie about the order. The fix stays `bfc new`.

**`bfc doctor` shows what `--fix` would do.** A failure that `--fix` can repair has a `Repair`, and its fix line reads `bfc doctor --fix: <what it does>; or <the way by hand>`. The output ends with the number of failed checks `--fix` can repair. A failure without a repair keeps a fix line that says why. When `--fix` tries a repair and it fails, the fix line still names `bfc doctor --fix`, to run again once the error is solved, or the way by hand.

## Considered

- **Fetch by object ID.** It fetches less, but depends on `uploadpack.allowAnySHA1InWant` or a similar setting on the server. Rejected for `--refetch`, which works everywhere.
- **Checking objects in every commit of the ref.** Rejected: that is `git fsck`, which issue #48 leaves out.
- **Always adding `format-version`** whatever the event types. Rejected: a v2 ref that lost the file would be marked v1.
