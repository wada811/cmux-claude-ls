# cmux-claude-surfaces

List the [cmux](https://www.cmux.dev/) surfaces (tabs) that run [Claude Code](https://claude.com/claude-code) and find which one needs you.

When you run many Claude Code sessions in cmux tabs, the tab titles do not tell you which one is waiting for your answer, which one is still working, and which one finished days ago. `cmux-claude-surfaces` lists every tab that runs (or ran) claude, across all windows and workspaces, with its state.

```console
$ cmux-claude-surfaces
WORKSPACE_REF  WORKSPACE_TITLE     SURFACE_REF   SURFACE_TITLE                     CWD                           STATE     AGE
workspace:1    Main                surface:1     ✳ Refactor the login flow         ~/src/app                     busy      now
workspace:1    Main                surface:3     Fix flaky test                    ~/src/app                     waiting   12m
workspace:2    Review              surface:7     PR #123                           ~/src/app--pr-123             stopped   2d
```

Use it to:

- **Find sessions waiting for you**: `STATE` is `waiting` while claude is blocked on a question or a permission prompt. `--state waiting` shows only those.
- **Clean up finished tabs**: `stopped` means claude has exited but the tab is still open; `--state stopped --older-than 14d` shows the ones that have not moved for two weeks.
- **Get from a tab to its session**: `--json` gives the session id and the transcript path, and `workspace_ref` / `surface_ref` are what `cmux` commands take.

It works well for LLM agents too: `--json` gives structured output, `--help` explains every field and state, and the exit status tells "nothing to list" (1) apart from "could not read cmux" (2), as `grep` does.

## Install

```sh
brew install wada811/tap/cmux-claude-surfaces
```

Or with Go:

```sh
go install github.com/wada811/cmux-claude-surfaces@latest
```

### Shell completion

For zsh, the Homebrew cask installs the completion script. Without Homebrew, generate it into a directory on your `fpath` (before `compinit`):

```sh
cmux-claude-surfaces --completion zsh > ~/.zfunc/_cmux-claude-surfaces
```

or load it from `.zshrc` after `compinit`:

```sh
source <(cmux-claude-surfaces --completion zsh)
```

It completes the flags and the values of `--state`, `--fields`, `--sort` and `--completion`; `--state` and `--fields` take a comma-separated list.

## Usage

### Fields

Each row has the fields below. Every field is a JSON key and, in upper case, a table header, so `--fields` takes the same names for both. The table shows the fields marked ✓ unless `--fields` is given; `--json` shows them all.

| Field | Table | Meaning |
| --- | --- | --- |
| `window_ref` | | cmux ref of the window. |
| `workspace_ref` | ✓ | cmux ref of the workspace. Pass it to the `--workspace` flag of cmux commands. |
| `workspace_title` | ✓ | Title of the workspace. |
| `surface_ref` | ✓ | cmux ref of the tab. Pass it to the `--surface` flag of cmux commands, e.g. `cmux read-screen --surface surface:7`. |
| `surface_title` | ✓ | Title of the tab. |
| `state` | ✓ | See below. |
| `waiting_for` | | What a `waiting` session is blocked on (see [JSON](#json)). `null` for other states and when the transcript cannot be read. The table shows one line, such as `question: <header>` or `permission: Bash go test ./...`. |
| `updated` | | Time of the last message. JSON: Unix seconds. Table: local time. `null` if there is no transcript. |
| `age` | ✓ | Time since the last message. JSON: seconds. Table: `12m`, `3h`, `2d`. |
| `cwd` | ✓ | Working directory of the session. The table shortens `$HOME` to `~`. |
| `session_id` | | Claude Code session id (the `checkpoint_id` of the cmux restore record). |
| `pid` | | Process id of the live session. `null` (table: `-`) when `state` is `stopped`. |
| `transcript_path` | | Path of the session's `.jsonl` transcript. `null` if it is not found. |

`session_id` and `transcript_path` are named like the input that Claude Code passes to hooks.

| `state` | Meaning |
| --- | --- |
| `busy` | Working on a reply. |
| `waiting` | Blocked on a question or a permission prompt. Needs you. |
| `idle` | Reply finished; at the prompt. |
| `shell` | Reply finished; background shells or agents are still running. |
| `stopped` | claude has exited; the tab is still open. |

Plain shell tabs are not listed.

### Options

Filters, `--sort` and `--fields` apply to the table and to `--json` alike.

| Flag | Meaning |
| --- | --- |
| `--state LIST` | Keep only these states. Comma-separated, from `busy`, `waiting`, `idle`, `shell`, `stopped`; for example `--state waiting,idle`. |
| `--older-than DURATION` | Keep only tabs whose last message is older than `DURATION`: `30m`, `12h`, `14d` (a whole number of days) or any [Go duration](https://pkg.go.dev/time#ParseDuration) such as `1h30m`. Tabs with no known last message are left out. |
| `--workspace WS` | Keep only the tabs of one workspace. Takes the same values as the `--workspace` flag of cmux commands (e.g. `workspace:3`). A value that matches nothing lists nothing and exits with 1; it is not a usage error. |
| `--exclude-self` | Leave out the tab this command runs in, so a Claude session that lists the others does not list itself. It uses the surface cmux reports for the `CMUX_SURFACE_ID` of the process. Outside cmux, where that variable is not set, it does nothing. |
| `--sort ORDER` | `tree` (default) is the order of the cmux sidebar. `state` puts the tabs that need you first: `waiting`, `idle`, `shell`, `busy`, `stopped`, newest message first within a state. `age` is newest message first. Ties keep the `tree` order. |
| `--completion SHELL` | Print the shell completion script and exit. Only `zsh` is supported; see [Shell completion](#shell-completion). |
| `--fields LIST` | Print only these fields, in this order, comma-separated; for example `--fields surface_ref,state,age`. Works for the table and for `--json`. An unknown name is a usage error (exit 2). |

### JSON

`--json` prints the same list as JSON, with every field unless `--fields` is given.

```console
$ cmux-claude-surfaces --json
[
 {
  "window_ref": "window:1",
  "workspace_ref": "workspace:1",
  "workspace_title": "Main",
  "surface_ref": "surface:1",
  "surface_title": "✳ Refactor the login flow",
  "state": "busy",
  "waiting_for": null,
  "updated": 1760000000,
  "age": 5,
  "cwd": "/Users/me/src/app",
  "session_id": "0b6f2c1e-5a43-4f0e-9a52-1d3c8e7b4a10",
  "pid": 12345,
  "transcript_path": "/Users/me/.claude/projects/-Users-me-src-app/0b6f2c1e-5a43-4f0e-9a52-1d3c8e7b4a10.jsonl"
 },
 {
  "window_ref": "window:1",
  "workspace_ref": "workspace:2",
  "workspace_title": "Review",
  "surface_ref": "surface:7",
  "surface_title": "PR #123",
  "state": "stopped",
  "waiting_for": null,
  "updated": 1759830000,
  "age": 170005,
  "cwd": "/Users/me/src/app--pr-123",
  "session_id": "7d2e9a40-8b1c-4c6d-b3f5-62a1c0e94d28",
  "pid": null,
  "transcript_path": "/Users/me/.claude/projects/-Users-me-src-app--pr-123/7d2e9a40-8b1c-4c6d-b3f5-62a1c0e94d28.jsonl"
 }
]
```

`waiting_for` has one of three shapes:

```json
{"kind": "question", "questions": [{"question": "Which one?", "header": "Choice", "options": ["A", "B"], "multi_select": false}]}
{"kind": "permission", "tool": "Bash", "input": "go test ./..."}
{"kind": "unknown"}
```

- `question`: an `AskUserQuestion` is open. `options` holds the labels.
- `permission`: a tool call awaits your approval. `input` is one short line: the command for `Bash`, the path for `Edit`, `Write` and `Read`, the input as JSON for other tools.
- `unknown`: the session is waiting, but no open question or tool call was found.

### Recipes

```sh
# Sessions waiting for your answer
cmux-claude-surfaces --state waiting

# What the waiting sessions are asking
cmux-claude-surfaces --state waiting --fields surface_ref,waiting_for
cmux-claude-surfaces --state waiting --json --fields surface_ref,waiting_for

# Tabs that finished and have not moved for 14 days (candidates to close)
cmux-claude-surfaces --state stopped --older-than 14d

# Everything, with the tabs that need you first
cmux-claude-surfaces --sort state

# The other tabs, as seen from a session that runs inside cmux
cmux-claude-surfaces --exclude-self --sort state

# A narrow table: leave out the workspace and the working directory
cmux-claude-surfaces --fields surface_ref,surface_title,state,age

# Only one workspace
cmux-claude-surfaces --workspace workspace:3 --state waiting

# Look at a listed tab, or read its transcript
cmux read-screen --surface surface:3
cmux-claude-surfaces --json | jq -r '.[] | select(.surface_ref == "surface:3") | .transcript_path' | xargs tail -n 5

# Close a listed tab (this tool only lists; closing is cmux's job).
# Take both refs from the same row: an unknown surface ref makes cmux close the current tab.
cmux close-surface --workspace <workspace_ref> --surface <surface_ref>
```

`cmux-claude-surfaces --help` prints all of this in short form.

## Exit status

| Code | Meaning |
| --- | --- |
| 0 | At least one row was printed. Warnings (for example a surface whose restore record could not be read) go to stderr and do not change the exit status. |
| 1 | Nothing to print: there is no Claude tab, or the filters left none. With `--json` it prints `[]`. |
| 2 | Error: invalid arguments, or cmux could not be read (not running, or not on `PATH`), or its output was in a format this tool does not understand. |

## How it works

1. `cmux tree --all --json` lists every terminal surface.
2. `cmux surface resume show` tells which surfaces have a Claude restore record, and its `checkpoint_id` is the Claude session id. The tty in `cmux tree` is not used: it outlives a closed tab and can be reused by another one.
3. `~/.claude/sessions/<pid>.json` (for live processes) gives the status and cwd, and `~/.claude/projects/*/<session>.jsonl` gives the last message time.
4. For `waiting` sessions, the last 256 KiB of the transcript gives `waiting_for`: the last `tool_use` that has no `tool_result` after it.

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
