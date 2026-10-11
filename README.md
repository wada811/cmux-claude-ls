# cmux-claude-ls

List the [cmux](https://www.cmux.dev/) surfaces that run (or ran) [Claude Code](https://claude.com/claude-code), across all windows and workspaces.

```console
$ cmux-claude-ls
WS#   WORKSPACE           SURFACE#  SURFACE                           CWD                           STATE     AGE
1     Main                1         ✳ Refactor the login flow         ~/src/app                     busy      now
1     Main                3         Fix flaky test                    ~/src/app                     idle      53m
2     Review              7         PR #123                           ~/src/app--pr-123             stopped   2d
```

- `STATE` is the live session status (`busy`, `idle`, `waiting`, ...) or `stopped` when the claude process has exited but the tab is still open.
- `AGE` is the time since the last message in the session.
- `WS#` / `SURFACE#` are the numbers you pass to `cmux` commands (e.g. `cmux read-screen --surface surface:7`).
- Plain shell tabs are not listed.

## Install

```sh
brew install wada811/tap/cmux-claude-ls
```

Or with Go:

```sh
go install github.com/wada811/cmux-claude-ls@latest
```

## Usage

```sh
cmux-claude-ls           # table
cmux-claude-ls --json    # JSON, including session id, pid and transcript path
```

## How it works

1. `cmux tree --all --json` lists every terminal surface.
2. `cmux surface resume show` tells which surfaces have a Claude restore record, and its `checkpoint_id` is the Claude session id. The tty in `cmux tree` is not used: it outlives a closed tab and can be reused by another one.
3. `~/.claude/sessions/<pid>.json` (for live processes) gives the status and cwd, and `~/.claude/projects/*/<session>.jsonl` gives the last message time.

## Requirements

- macOS with cmux running
- Claude Code launched inside cmux surfaces

## Release

```sh
git tag vX.Y.Z && git push origin vX.Y.Z && GITHUB_TOKEN=$(gh auth token) goreleaser release --clean
```

## 日本語

cmux の全ウィンドウ・全ワークスペースから、Claude Code が動いている（動いていた）surface を一覧するコマンドです。

- `STATE` はセッションの状態（`busy`・`idle` など）。claude が終了してタブだけ残っているものは `stopped` と出ます。
- `AGE` はセッションの最後のメッセージからの経過時間です。
- `WS#`・`SURFACE#` は `cmux` コマンドに渡す番号です。
- ただのシェルのタブは出しません。

インストールは `brew install wada811/tap/cmux-claude-ls`。surface とセッションの対応は cmux の再開記録（`checkpoint_id`）で取ります。`cmux tree` の tty はタブを閉じた後も残り、別のタブで使い回されることがあるため使いません。

## License

MIT
