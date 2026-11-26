---
author:
  name: admSla99
  email: 188631245+admSla99@users.noreply.github.com
at: 2026-11-26T22:44:28Z
agent: claude-code
to: [188631245+admSla99@users.noreply.github.com]
ref: intent.md
options: ['Install it, then warn if the new binary cannot write the ref''s format-version', Refuse unless --force when the release is older than the ref's format-version, No check; bfc doctor reports it afterwards]
recommended: Install it, then warn if the new binary cannot write the ref's format-version
---

bfc update <version> can roll back to a release that cannot write the plans ref's format-version (ADR 0002: it then refuses to write). What should bfc update do?
