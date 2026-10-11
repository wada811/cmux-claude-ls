package main

const usageLine = "usage: cmux-claude-surfaces [--state LIST] [--older-than DURATION] [--workspace WS] [--exclude-self] [--sort tree|state|age] [--fields LIST] [--json] [--completion SHELL] [--version]"

// helpText is written for both people and LLM agents: what the tool is for,
// what each value means and what to do next.
const helpText = `cmux-claude-surfaces - list the cmux surfaces (tabs) that run Claude Code and find which one needs you.

When many Claude Code sessions run in cmux tabs, this lists every tab that
runs (or ran) claude, across all windows and workspaces, with its state.
Use it to:
  - find sessions waiting for your answer        (--state waiting)
  - find finished tabs that can be closed        (--state stopped --older-than 14d)
  - get a tab's session id and transcript path   (--json, or
                                                  --fields session_id,transcript_path)

` + usageLine + `

Options:
  --state LIST           keep only these states, comma-separated, from busy,
                         waiting, idle, shell, stopped (e.g. waiting,idle)
  --older-than DURATION  keep only tabs whose last message is older than
                         DURATION: 30m, 12h, 14d or any Go duration (1h30m).
                         Tabs with no known last message are left out.
  --workspace WS         keep only this workspace; takes the same values as
                         the --workspace flag of cmux commands (workspace:3).
                         An unknown value lists nothing and exits with 1.
  --exclude-self         leave out the tab this command runs in (found through
                         cmux's CMUX_SURFACE_ID; does nothing outside cmux)
  --sort ORDER           tree   cmux sidebar order (default)
                         state  waiting, idle, shell, busy, stopped; newest
                                first within a state
                         age    newest last message first
  --fields LIST          print only these fields, in this order,
                         comma-separated (e.g. surface_ref,state,age); see
                         Fields. Works for the table and for --json
  --json                 print the list as JSON, every field unless --fields
  --completion SHELL     print the shell completion script and exit; SHELL is
                         zsh (see Shell completion)
  --version              print the version and exit
  -h, --help             print this help

Filters, --sort and --fields apply to the table and to --json alike.

Fields:
  Each field is a JSON key and, in upper case, a table header. The table
  shows the ones marked * unless --fields is given; JSON shows them all.

  window_ref         cmux ref of the window
  workspace_ref *    cmux ref of the workspace
  workspace_title *  title of the workspace
  surface_ref *      cmux ref of the tab
  surface_title *    title of the tab
  state *            busy     working on a reply
                     waiting  blocked on a question or a permission prompt
                     idle     reply finished; at the prompt
                     shell    reply finished; background shells or agents
                              still run
                     stopped  claude has exited; the tab is still open
  waiting_for        what a waiting session is blocked on (see below); the
                     table shows one line such as "question: <header>"
  updated            time of the last message (JSON: Unix seconds; table:
                     local time)
  age *              time since the last message (JSON: seconds; table: 3h)
  cwd *              working directory (the table shortens $HOME to ~)
  session_id         Claude Code session id
  pid                process id of claude while it runs, else null
                     (table: -)
  transcript_path    the session's .jsonl transcript

  session_id and transcript_path are named like the input of Claude Code
  hooks.

waiting_for says what a "waiting" session is blocked on, read from the end of
its transcript. It is null for other states and when the transcript cannot be
read.
  {"kind": "question", "questions": [{"question": "...", "header": "...",
     "options": ["label", ...], "multi_select": false}]}
                         an AskUserQuestion is open
  {"kind": "permission", "tool": "Bash", "input": "<command or path>"}
                         a tool call awaits your approval; input is the
                         command (Bash), the file path (Edit, Write, Read) or
                         the tool input as one short line of JSON
  {"kind": "unknown"}    waiting, but no open question or tool call was found

Examples:
  cmux-claude-surfaces
  cmux-claude-surfaces --state waiting
  cmux-claude-surfaces --state stopped --older-than 14d
  cmux-claude-surfaces --sort state
  cmux-claude-surfaces --exclude-self --sort state
  cmux-claude-surfaces --fields surface_ref,surface_title,state,age
  cmux-claude-surfaces --state waiting --fields surface_ref,waiting_for
  cmux-claude-surfaces --workspace workspace:3 --state waiting
  cmux-claude-surfaces --state waiting --json --fields surface_ref,waiting_for
  cmux-claude-surfaces --state waiting --json | jq -r '.[].transcript_path'
  cmux read-screen --surface surface:9   # look at a listed tab
  cmux close-surface --workspace workspace:2 --surface surface:7   # close one

Shell completion (zsh):
  Homebrew installs it. Otherwise put the script on your fpath, then run
  compinit, or load it in .zshrc:
    cmux-claude-surfaces --completion zsh > ~/.zfunc/_cmux-claude-surfaces
    source <(cmux-claude-surfaces --completion zsh)
  It completes the flags and the values of --state, --fields, --sort and
  --completion. --state and --fields take a comma-separated list.

Exit status (like grep):
  0  at least one row was printed
  1  no row to print: no Claude tab, or the filters left none ("[]" with --json)
  2  error: bad usage, or cmux or its output could not be read
Warnings (e.g. a tab whose restore record could not be read) go to stderr and
do not change the exit status.

Tabs are matched to sessions by cmux's restore record (checkpoint_id), not by
tty, which outlives a closed tab and can be reused by another one.`
