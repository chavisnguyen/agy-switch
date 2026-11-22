# agys

**Multi-account manager and auto-failover runner for Google Antigravity CLI (`agy`).**

When `agy` hits a rate limit (HTTP 429 / `RESOURCE_EXHAUSTED`), you normally have to switch accounts,
copy tokens and restart the conversation by hand. Save your Google accounts once as profiles, then
`agys` switches between them, shows each account's quota, and can keep your session going when one
account runs out.

One small Go binary, standard library only. No telemetry: tokens never leave your disk.

## Install

```bash
brew tap chavisnguyen/agy-switch https://github.com/chavisnguyen/agy-switch
brew install agys
```

No Homebrew? Download the binary for your machine:

```bash
curl -fsSL https://raw.githubusercontent.com/chavisnguyen/agy-switch/master/install.sh | bash
```

Or build it (Go 1.22+): `git clone https://github.com/chavisnguyen/agy-switch && cd agy-switch && make install`.

Already installed another way? Remove the old binary first (`rm /opt/homebrew/bin/agys`), or `brew install` will report a conflict.

## Quick start

**1. Once: save the account you are logged in with, and add the others.**

```bash
agys save main          # the account agy is using right now
agys login work         # log in with another Google account and save it as "work"
```

`login` sets your current session aside, opens `agy` for the browser login, saves the new profile and puts your session back.

**2. See every account's quota.**

```bash
agys status
```

```text
📊 AGY ACCOUNTS STATUS & QUOTA
══════════════════════════════════════════════════════════
[1] work (● ACTIVE)
    Email  : work.developer@gmail.com
    Claude 5h 🟢 [████████████████████] 100.0%  in 4h59m (16:46)
    Claude 1w 🟢 [██████████░░░░░░░░░░]  50.0%  6d17h left (29 Nov)
    Gemini 5h 🟢 [████████████████████] 100.0%  in 4h59m (16:46)
    Gemini 1w 🟢 [████████████████████] 100.0%  6d17h left (29 Nov)
    ──────────────────────────────────────────────────
[2] personal (○ Ready)
    Email  : personal@gmail.com
    Claude 5h 🟢 [████████████████████] 100.0%  blocked by weekly cap
    Claude 1w 🔴 [░░░░░░░░░░░░░░░░░░░░]   0.0%  2d07h left (25 Nov)
    Gemini 5h 🟢 [████████████████████] 100.0%  in 4h59m (16:46)
    Gemini 1w 🟢 [████████████████████]  98.2%  5d18h left (28 Nov)
══════════════════════════════════════════════════════════
```

Each model has two limits: a **5-hour** window and a **weekly** one. When the weekly limit is used up,
the 5-hour line says `blocked by weekly cap`, because the 5h bar does not apply any more.

**3. Switch, or let `agys` choose.**

```bash
agys main        # switch to a profile
agys next        # switch to the best account right now
agys             # pick from a numbered menu
```

**4. Run `agy` with auto-failover.**

```bash
agys run
```

That's all most people need.

---

## When you hit a rate limit

`agys run` starts `agy` and watches its log. On a 429 it:

1. puts the account that session was using into a 30-minute cooldown,
2. switches to the next healthy account (same choice as `agys next`),
3. resumes your conversation with `--continue`, so you keep your context.

```bash
agys run                                   # new task
agys run --continue                        # continue the last conversation
agys run --conversation=<id>               # continue a specific one
```

When the session ends, `agys run` prints the exact command to pick it up again.

### Running several windows

You can run `agys run` in many terminals at once; each window is handled on its own.

- Each window gets its own log (`~/.gemini/antigravity-cli/log/agys-run-*.log`), so one window's 429 never restarts another. If your `agy` ignores `--log-file`, `agys` warns and falls back to the shared `cli.log`.
- The cooldown goes to the account that window started with, even if another window has switched since.
- Switches are serialized with a lock. If another window already moved to a healthy account, yours resumes on it instead of switching again.

> **Note:** the active account is machine-wide (one token file, one Keychain entry), so windows tend to
> end up on the same account after a failover. Separate accounts per window are not supported.

## How `agys next` picks an account

Quota resets are "use it or lose it": 70% left with 30 minutes to the reset is wasted if you don't use it.
`agys next` (and failover) reads every account's quota and picks, in this order:

1. **Expiring soon:** an account whose 5h window resets in under 90 minutes with at least 10% left, so it gets used before it resets.
2. **Not weekly-capped:** accounts whose weekly limit is used up (under 5% left) are skipped for Claude.
3. **Balanced:** among the rest, prefer the one that spreads usage, so no single account hits its weekly cap early.
4. **Gemini fallback:** if Claude is exhausted everywhere, an account with Gemini quota left.

Accounts in cooldown are never picked. `agys burn` does the same selection.

## Resume a conversation

```bash
agys conv                 # conversations for this folder
```

```text
💬 CONVERSATIONS FOR: /Users/me/projects/my-app
════════════════════════════════════════════════════════════════════════════════════════════
    ID    TITLE                                  STEPS   LAST ACTIVE        CONV ID
────────────────────────────────────────────────────────────────────────────────────────────
❯   [1]   Fix login redirect                     524     Just now           6c04bd0d…          ●
    [2]   Checking status command usage          770     3h ago             f51a3479…
    [3]   General greeting                       71      23h ago            0d816ff0…
════════════════════════════════════════════════════════════════════════════════════════════
● = current session for this folder
```

Keys: `↑` `↓` (or `j` `k`) move · `1`-`9` jump · `enter` resume with auto-failover · `q` cancel.

| Run | It does |
|---|---|
| `agys conv 2` | resume conversation #2 right away |
| `agys conv /path/to/project` | list another folder's conversations |
| `agys conv -a` | list recent conversations from every folder |
| `agys conv -l` | just list, no prompt |

## Something not right?

| Run | It tells you |
|---|---|
| `agys status` | each account's 5h and weekly quota, and which one is active or in cooldown |
| `agys quota-raw [profile]` | the raw quota response from Google, for when the numbers look wrong |
| `agys cooldown` | accounts currently cooling down (`agys cooldown clear` resets them) |
| `agys version` | the installed version |

## Good to know

- **Tokens** are stored in `~/.gemini/antigravity-cli/profiles/`, readable only by you (`0600`). Switching writes to a temp file and renames it, so an interruption can't corrupt a token.
- **Keychain:** on macOS, switching also updates the `gemini` / `antigravity` Keychain entry, so the CLI and the IDE stay on the same account.
- **Quota checks** try IPv4 first, so a broken IPv6 route doesn't make `agys status` hang.
- **Tab title:** while `agys run` is active, your terminal tab shows the conversation's topic (Ghostty, CMUX, iTerm2, Kitty, macOS Terminal).
- **5h reset times** can look identical across accounts. That is what Google returns; use `agys quota-raw` to see it.
- Google OAuth client credentials are obfuscated in the binary and resolved at run time; nothing secret is stored in this repository.

## All commands

| Command | What it does |
|---|---|
| `agys status` (or `ls`) | all accounts with quota, 5h and weekly |
| `agys <profile>` | switch to a profile |
| `agys next` / `agys burn` | switch to the best account |
| `agys` | numbered menu to switch accounts |
| `agys run [args...]` | run `agy` with auto-failover and resume |
| `agys conv [dir]` | pick and resume a conversation |
| `agys login [name]` | log in with a Google account in the browser and save it |
| `agys save [name]` | save the current session as a profile |
| `agys rename <old> <new>` | rename a profile |
| `agys remove <name>` | delete a profile |
| `agys cooldown [clear]` | show cooldowns / reset them |
| `agys quota-raw [profile]` | print Google's raw quota response |
| `agys install` | add shortcut symlinks (`/opt/homebrew/bin` or `~/.local/bin`) |
| `agys update` | update to the latest release (on Homebrew, tells you to use `brew upgrade`) |
| `agys version` / `agys help` | version / built-in help |

## Update

```bash
brew update && brew upgrade agys      # installed with Homebrew
agys update                           # installed with install.sh or make install
```

`agys status` also tells you when a newer release is available.

## Uninstall

```bash
brew uninstall agys && brew untap chavisnguyen/agy-switch
```

Your saved profiles stay in `~/.gemini/antigravity-cli/profiles/` (they hold your login tokens). Delete that folder too if you want them gone.
