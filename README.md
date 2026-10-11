# cmux-claude-ls

Find which [Claude Code](https://claude.com/claude-code) session in [cmux](https://www.cmux.dev/) needs you.

When you run many Claude Code sessions in cmux tabs, the tab titles do not tell you which one is waiting for your answer, which one is still working, and which one finished days ago. `cmux-claude-ls` lists every tab that runs (or ran) claude, across all windows and workspaces, with its state.

```console
$ cmux-claude-ls
WS#   WORKSPACE           SURFACE#  SURFACE                           CWD                           STATE     AGE
1     Main                1         ✳ Refactor the login flow         ~/src/app                     busy      now
1     Main                3         Fix flaky test                    ~/src/app                     waiting   12m
2     Review              7         PR #123                           ~/src/app--pr-123             stopped   2d
```

Use it to:

- **Find sessions waiting for you**: `STATE` is `waiting` while claude is blocked on a question or a permission prompt.
- **Clean up finished tabs**: `stopped` means claude has exited but the tab is still open; `AGE` tells how long ago the session last moved.
- **Get from a tab to its session**: `--json` gives the session id and the transcript path, and `WS#` / `SURFACE#` are what `cmux` commands take.

It works well for LLM agents too: `--json` gives structured output, `--help` explains every column and state, and the exit status tells "no Claude tabs" apart from "could not read cmux".

## Install

```sh
brew install wada811/tap/cmux-claude-ls
```

Or with Go:

```sh
go install github.com/wada811/cmux-claude-ls@latest
```

## Usage

### Table

| Column | Meaning |
| --- | --- |
| `WS#`, `SURFACE#` | cmux refs. Pass them as `workspace:N` / `surface:N` to `cmux` commands. |
| `WORKSPACE`, `SURFACE` | Titles of the workspace and the tab. |
| `CWD` | Working directory of the session. |
| `STATE` | See below. |
| `AGE` | Time since the last message in the session. |

| `STATE` | Meaning |
| --- | --- |
| `busy` | Working on a reply. |
| `waiting` | Blocked on a question or a permission prompt. Needs you. |
| `idle` | Reply finished; at the prompt. |
| `shell` | Reply finished; background shells or agents are still running. |
| `stopped` | claude has exited; the tab is still open. |

Plain shell tabs are not listed.

### JSON

Prints the same list as JSON, with the session id, pid and transcript path added. A `stopped` surface has `null` for `pid`.

```console
$ cmux-claude-ls --json
[
 {
  "window": "window:1",
  "workspace": "workspace:1",
  "workspace_title": "Main",
  "surface": "surface:1",
  "title": "✳ Refactor the login flow",
  "state": "busy",
  "updated": 1760000000,
  "cwd": "/Users/me/src/app",
  "session": "0b6f2c1e-5a43-4f0e-9a52-1d3c8e7b4a10",
  "pid": 12345,
  "jsonl": "/Users/me/.claude/projects/-Users-me-src-app/0b6f2c1e-5a43-4f0e-9a52-1d3c8e7b4a10.jsonl"
 },
 {
  "window": "window:1",
  "workspace": "workspace:2",
  "workspace_title": "Review",
  "surface": "surface:7",
  "title": "PR #123",
  "state": "stopped",
  "updated": 1759830000,
  "cwd": "/Users/me/src/app--pr-123",
  "session": "7d2e9a40-8b1c-4c6d-b3f5-62a1c0e94d28",
  "pid": null,
  "jsonl": "/Users/me/.claude/projects/-Users-me-src-app--pr-123/7d2e9a40-8b1c-4c6d-b3f5-62a1c0e94d28.jsonl"
 }
]
```

| Key | Meaning |
| --- | --- |
| `window`, `workspace`, `surface` | cmux refs. Pass them to `cmux` commands, e.g. `cmux read-screen --surface surface:7`. |
| `workspace_title`, `title` | Titles of the workspace and the surface. |
| `state` | Live session status (`busy`, `idle`, `waiting`, ...), or `stopped` when the claude process has exited but the tab is still open. |
| `updated` | Time of the last message in the session, as Unix seconds. `null` if there is no transcript. |
| `cwd` | Working directory of the session. |
| `session` | Claude session id (the `checkpoint_id` of the restore record). |
| `pid` | Process id of the live session. `null` when `state` is `stopped`. |
| `jsonl` | Path of the session transcript. `null` if it is not found. |

### Recipes

```sh
# Sessions waiting for your answer
cmux-claude-ls --json | jq '.[] | select(.state == "waiting")'

# Tabs that have not moved for 14 days (candidates to close)
cmux-claude-ls --json | jq --argjson t "$(date -v-14d +%s)" '.[] | select(.updated < $t)'

# Look at a listed tab, or read its transcript
cmux read-screen --workspace workspace:1 --surface surface:3
cmux-claude-ls --json | jq -r '.[] | select(.surface == "surface:3") | .jsonl' | xargs tail -n 5
```

`cmux-claude-ls --help` prints all of this in short form.

## Exit status

| Code | Meaning |
| --- | --- |
| 0 | Success. Warnings (for example a surface whose restore record could not be read) go to stderr and do not change the exit status. |
| 1 | cmux could not be read (not running, or not on `PATH`), or its output was in a format this tool does not understand. |
| 2 | Invalid arguments. |

## How it works

1. `cmux tree --all --json` lists every terminal surface.
2. `cmux surface resume show` tells which surfaces have a Claude restore record, and its `checkpoint_id` is the Claude session id. The tty in `cmux tree` is not used: it outlives a closed tab and can be reused by another one.
3. `~/.claude/sessions/<pid>.json` (for live processes) gives the status and cwd, and `~/.claude/projects/*/<session>.jsonl` gives the last message time.

If the output of cmux or Claude Code cannot be read any more (for example after a format change), it exits with an error instead of printing an empty table, and asks you to report it.

## Requirements

- macOS with cmux running
- Claude Code launched inside cmux surfaces

## Release (maintainers)

```sh
git tag vX.Y.Z && git push origin vX.Y.Z && GITHUB_TOKEN=$(gh auth token) goreleaser release --clean
```

## License

MIT
