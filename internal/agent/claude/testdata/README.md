# Recorded Claude Code output

The `*.jsonl` files are the standard output of a real Claude Code 2.1.328
(`claude --version`), recorded once on 2026-11-19 for issue #59 in an empty
git repository holding `notes.txt` with the word `hello`. The tests replay
them through `fakeclaude/`; no test starts the real Claude Code.

| File | Command | Exit |
| --- | --- | --- |
| `hello.jsonl` | `claude -p "Reply with exactly: hi" --output-format stream-json --verbose` | 0 |
| `tool.jsonl` | `claude -p "Read notes.txt with the Read tool and tell me its first word." --output-format stream-json --verbose --resume <session of hello.jsonl> --allowedTools Read` | 0 |
| `badresume.jsonl` | `claude -p "hi" --output-format stream-json --verbose --resume 00000000-0000-4000-8000-000000000000` | 1, with `badresume.stderr` on standard error |

Changes made after recording, so that the files hold nothing from the
recording machine:

- The `system`/`init` line keeps only `type`, `subtype`, `cwd`,
  `session_id`, `model`, `permissionMode`, `claude_code_version`, `uuid`
  and the built-in tools; the machine's plugins, skills, MCP servers and
  paths are removed.
- The recording directory is replaced by `/work`, also in the
  `partial_json` chunks of `tool.jsonl`.

The recording environment emits `stream_event` lines (partial messages) and
other line types such as `rate_limit_event`; the adapter ignores the types it
does not use.

What the recording showed about the command line of Claude Code 2.1.328:

- `-p` (`--print`) is a boolean flag; the prompt is a positional argument.
  `--allowedTools` takes several values, so the adapter ends the options
  with `--` and puts the prompt after it. This was checked with the real
  binary: `claude -p --output-format stream-json --verbose --allowedTools
  Read -- "--help: reply with exactly ok"` answered `ok`.
- `--output-format stream-json` needs `--verbose` with `-p`.
- Each line is one JSON object with a `type`. The adapter reads
  `assistant` (`message.content` blocks of type `text` and `tool_use`) and
  `result` (`result`, `is_error`, `errors`, `session_id`). Every line
  carries `session_id`; `--resume` with the session of the previous result
  continues it.
- A failed `--resume` prints a `result` line with `is_error: true` and the
  message in `errors`, writes the same message to standard error and exits 1.
