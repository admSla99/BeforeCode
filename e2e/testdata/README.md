# Recorded PreToolUse inputs

`pretooluse-edit.json` and `pretooluse-write.json` are the standard input
that a real Claude Code 2.1.329 (`claude --version`) gave a `PreToolUse`
hook, recorded once on 2026-11-20 for issue #67. The hook only appended its
input to a file. The recording repository held `notes.txt` with the word
`hello`, and the commands were:

| File | Command |
| --- | --- |
| `pretooluse-edit.json` | `claude -p "Use the Edit tool to change hello to bye in notes.txt" --permission-mode acceptEdits` |
| `pretooluse-write.json` | `claude -p "Use the Write tool to create main.go containing exactly: package main" --permission-mode acceptEdits` |

Changes made after recording, so that the files hold nothing from the
recording machine: the recording directory is replaced by `/work` in `cwd`
and `tool_input.file_path`, and by `-work` in `transcript_path` and
`scratchpad_dir`. The test replaces `cwd` with the clone and moves
`file_path` into it.
