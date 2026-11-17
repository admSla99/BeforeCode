# ADR 0003: `.beforecode/config.yaml` version 1

Date: 2026-11-17. Status: accepted. Settled with the maintainer for issue #8 (`bfc init`).

## Decision

`.beforecode/config.yaml` lives on normal branches (ADR 0001) and changes through pull requests. Version 1 has exactly these keys:

```yaml
# BeforeCode configuration. Change it through a pull request.
version: 1

approval:
  # Approvals by people other than the plan author needed before a plan is approved.
  required_approvals: 1

sync:
  # false: plans stay in this clone and are never pushed (bfc init --local).
  enabled: true
  # The git remote that receives refs/beforecode/plans. Only when enabled.
  remote: origin
```

| Key | Type | Rule |
| --- | --- | --- |
| `version` | integer, required | `1`. A greater value makes `bfc` stop with "update bfc". |
| `approval.required_approvals` | integer ≥ 1, required | Copied into each `submitted` event at submit time (ADR 0002, section 4). Same name as that field. |
| `sync.enabled` | boolean, required | `false` is local mode. |
| `sync.remote` | string | Required when `sync.enabled` is `true`, absent when it is `false`. The **name** of a git remote, not its URL. |

- **Strict reading.** An unknown key is an error that names the key. A misspelt approval policy must not be silently ignored.
- **Generated with comments.** `bfc init` writes the file with short comments, as above, because people edit it in pull requests.
- **Code.** Parsing and validation live in `internal/config`, which reads bytes and does no git I/O. The YAML library is `go.yaml.in/yaml/v3` with known-field checking.

## `bfc init` behaviour that follows

- `sync.remote` defaults to `origin`; `--remote <name>` picks another. If the remote does not exist, `init` fails and suggests `--remote` or `--local`.
- Before creating the plans ref, `init` runs `git ls-remote <remote> refs/beforecode/plans`. If the ref is there, `init` refuses and points to `bfc bootstrap`. If the remote cannot be reached, `init` fails and suggests retrying or `--local`, because skipping the check could create a second, unrelated plans ref.
- `init` never pushes. It ends by telling the user to commit `.beforecode/` and run `bfc push`.
- `init` never overwrites. An existing `config.yaml`, template or plans ref is kept and reported as already there. The fetch refspec is added only when the config in place has `sync.enabled: true` and the refspec is missing.
- The default templates are embedded in the binary from this repository's own `.beforecode/templates/` through a small package at the module root, so there is one copy of each file.

## Considered options

- **A `harnesses` section now.** `docs/design.md` mentions it, but nothing reads it before phase 2. Added then, in a new ADR.
- **Sync off as an empty or missing `sync.remote`, or a `sync.mode` string.** Rejected; an explicit boolean is the clearest to read in a pull request.
- **Remote URL instead of name.** Rejected because the URL differs between clones (SSH or HTTPS), while the remote name is shared.
- **Local mode in `.git/config` instead of `config.yaml`.** Rejected to keep the issue's design: local mode is for a solo repository where nothing is shared.
- **Canonical templates under `internal/`, kept equal to `.beforecode/templates/` by a test.** Rejected; two copies of the same files with a test to keep them equal is more to maintain than one embed package.

## Consequences

- Local mode is committed, so everyone who clones the repository also gets `sync.enabled: false`. Turning sync on later is a pull request that changes this file, followed by `bfc init` again to add the refspec.
- Changing `required_approvals` does not change the state of plans already submitted, because the count is fixed in each `submitted` event.
- Every new key bumps `version`, so an older binary stops with "update bfc" instead of an unknown-key error.
