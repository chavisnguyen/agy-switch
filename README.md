# 🚀 agys

<p align="center">
  <b>A blazing-fast, lightweight, and zero-dependency multi-account manager & auto-failover runner for Google Antigravity CLI (<code>agy</code>).</b>
</p>

<p align="center">
  <img src="https://img.shields.io/github/actions/workflow/status/chavisnguyen/agy-switch/ci.yml?branch=master&style=flat-square&label=CI" alt="CI" />
  <img src="https://img.shields.io/badge/Language-Go%201.22+-00ADD8?style=flat-square&logo=go" alt="Go" />
  <img src="https://img.shields.io/badge/Platform-macOS%20%7C%20Linux-black?style=flat-square&logo=apple" alt="Platforms" />
  <img src="https://img.shields.io/badge/License-MIT-green?style=flat-square" alt="License" />
  <img src="https://img.shields.io/badge/Dependencies-Zero-blue?style=flat-square" alt="Zero Dependencies" />
</p>

---

## 🌟 Why `agys`?

Google Antigravity (`agy`) is powerful, but when hitting rate limits (HTTP 429 / `RESOURCE_EXHAUSTED`), manually switching accounts, copying tokens, or restarting conversations disrupts your flow.

**`agys`** solves this with a standalone, native Go binary (compiled with standard library only):
- ⚡ **Instant Switching**: Switch profiles in under a second with atomic file writes and macOS Keychain sync.
- 📊 **Real-Time Quotas**: Check Claude Sonnet and Gemini quotas + reset schedules for all your accounts at a glance.
- 🔄 **Smart Auto-Failover**: Run `agys run` to automatically rotate to a healthy account when hitting rate limits and resume your conversation without losing context (`--continue`).
- 🏷️ **Dynamic Tab Titles**: Keeps your terminal tab title synced with the actual chat topic (supports Ghostty, CMUX, iTerm2, Kitty, and macOS Terminal).
- ⏱️ **Intelligent Cooldowns**: Automatically cools down exhausted accounts for 30 minutes to prevent switching cycles.
- 🔒 **100% Private & Secure**: Tokens stay strictly on your local disk with `0600` permissions. No telemetry, no third-party servers.

---

## 📦 Quick Installation

### Method 1: One-Line Install (No Go Required ⚡)
Run this single command in your terminal. It automatically detects your machine's OS/architecture and downloads the pre-built binary:
```bash
curl -fsSL https://raw.githubusercontent.com/chavisnguyen/agy-switch/master/install.sh | bash
```

### Method 2: Build From Source (Requires Go 1.22+)
```bash
git clone https://github.com/chavisnguyen/agy-switch.git
cd agy-switch
make install
```
This compiles the optimized native binary and installs `agys` globally into `/opt/homebrew/bin/agys` (or `~/.local/bin/agys`).

### 🔄 Keeping `agys` Up to Date
Upgrade to the newest release anytime with a single command:
```bash
agys update
```
*(You will also get a friendly notification when running `agys status` whenever a new release is available).*

---

## 📖 Command Reference

### 🚀 Run & Code
| Command | Description |
| :--- | :--- |
| **`agys run [args...]`** | Smart runner: runs `agy` with auto-failover & context resume on rate limits |
| **`agys conv [dir]`** | Interactive picker (`↑`/`↓`): select & resume conversations in current or target folder |

### 📊 Accounts & Quotas
| Command | Description |
| :--- | :--- |
| **`agys status`** (or `ls`) | Unified view of all accounts, real-time quotas, 5h cycles & Weekly Cap status |
| **`agys next`** | Auto-switch to the optimal account (smart burn expiring tokens + load balancing) |
| **`agys <profile>`** | Instantly switch to the specified profile |
| **`agys`** | Interactive numbered menu to select and switch accounts |

### ⚙️ Profile Management
| Command | Description |
| :--- | :--- |
| `agys login [name]` | Authenticate a new Google account in browser and save profile |
| `agys save [name]` | Save current active session as a named profile |
| `agys rename <old> <new>` | Rename an existing saved profile |
| `agys remove <name>` | Delete a saved profile |

### 🛠️ Utility
| Command | Description |
| :--- | :--- |
| `agys update` | Automatically update `agys` to the latest GitHub release |
| `agys version` | Show current installed version & architecture |
| `agys help` | Display built-in help message |

---

## 🚀 Quick Walkthrough

### 1. Save Your Existing Active Account
If you already logged into `agy`:
```bash
agys save main
```

### 2. Add Additional Google Accounts
Use the login wizard to add more accounts without losing your existing session:
```bash
agys login work
```
*The wizard temporarily stashes your active session, launches `agy` for Google browser login, saves the new profile as `work`, and restores cleanly.*

### 3. Check Real-Time Quotas (5h Cycle vs 7-Day Weekly Cap)
```bash
agys status
```
Output preview:
```text
📊 AGY ACCOUNTS STATUS & QUOTA
══════════════════════════════════════════════════════════
[1] work (● ACTIVE)
    Email  : work.developer@gmail.com
    Claude : 🟢 100.0% (resets 16:46 in 4h59m [5h])
    Gemini : 🟢 100.0% (resets 16:46 in 4h59m [5h])
    ──────────────────────────────────────────────────
[2] personal (○ Ready)
    Email  : personal@gmail.com
    Claude : 🟢 100.0% (resets 16:46 in 4h59m [5h])
    Gemini : 🟢  79.8% (resets 12:43 in 56m [5h])
    ──────────────────────────────────────────────────
[3] main (🔴 Weekly Cap)
    Email  : main.dev@gmail.com
    Claude : 🔴   0.0% (resets 02 Nov 21:20, 1d left [Weekly Cap ⚠️])
    Gemini : 🟢 100.0% (resets 16:46 in 4h59m [5h])
══════════════════════════════════════════════════════════
```

> [!NOTE]
> `agys` automatically detects whether an account is in the standard **5-hour burst cycle** (`[5h]`) or has reached Google's **7-day protective limit** (`[Weekly Cap ⚠️]`), preventing blind switches into dead accounts!

### 4. Switch Accounts On The Fly
```bash
# Switch directly to an account
agys main

# Or rotate to next optimal account (smart burn & load balanced)
agys next

# Or open interactive menu to pick by number
agys
```

---

## 🧠 Smart Quota Optimization (Built into `agys next` & `agys run`)

Google Antigravity quotas operate on a **"Use-It-Or-Lose-It"** model. If an account has 70% quota remaining with 30 minutes until reset, that quota evaporates if unused. 

Whenever you run **`agys next`** or whenever **`agys run`** encounters a 429 rate limit, it automatically inspects quotas in real-time and picks the optimal account:

### How Smart Selection Works:
1. **Urgent Burn**: If an account is expiring in `< 90m` with `>= 10%` quota, it prioritizes switching to it so no tokens are wasted before the reset window closes.
2. **Weekly Cap Protection**: Accounts with `0.0%` quota and multi-day reset locks (`[Weekly Cap ⚠️]`) are strictly skipped — no more blind switches into dead accounts!
3. **Balanced Load**: When accounts have ample time, it balances usage across 5h accounts so no single account prematurely triggers the 7-day weekly cap.
4. **Zero Configuration**: This intelligence runs automatically in `agys next` and `agys run` without requiring any extra flags. (You can also run `agys burn` as an alias).

---

## 💬 Interactive Conversation Picker (`agys conv`)

Resume conversations for your current workspace without hunting for UUIDs:

```bash
agys conv
```

```text
💬 CONVERSATIONS FOR: /Users/chavis/Documents/personal/tool/agy-switch
════════════════════════════════════════════════════════════════════════════════════════════
    ID    TITLE                                  STEPS   LAST ACTIVE        CONV ID           
────────────────────────────────────────────────────────────────────────────────────────────
❯   [1]   Agy Switch Rate Limit Error            524     Just now           6c04bd0d…          ●
    [2]   Checking Status Command Usage          770     3h ago             f51a3479…         
    [3]   General Greeting                       71      23h ago            0d816ff0…         
    [4]   Simple Greeting                        2       Yesterday          c1fd6d46…         
    [5]   AI Model Identification                1       Yesterday          8ab7b921…         
────────────────────────────────────────────────────────────────────────────────────────────
● indicates current active session for this folder
↑/↓ or j/k: navigate • Enter: resume • 1-9: jump • q/Esc: cancel
```

### Controls & Options:
* **`↑` / `↓`** (or **`j` / `k`**): Smoothly navigate up and down with loop-around.
* **`Enter`**: Instantly resume the highlighted conversation with auto-failover enabled (`agys run --conversation=<id>`).
* **`1` – `9`**: Jump directly to a conversation number.
* **`q` / `Esc`**: Cancel and exit cleanly.
* **Resume immediately by number**: `agys conv 2` (resumes conversation #2 directly).
* **View other workspaces**: `agys conv /path/to/other-project` or `agys conv other-project`.
* **View all recent chats globally**: `agys conv -a` (or `--all`).
* **List only without prompt**: `agys conv -l`.

---

## ⚡ Smart Runner (`agys run`)

Run `agy` through `agys run` for uninterrupted development:
```bash
# Start a new task
agys run

# Or continue a previous conversation
agys run --continue
agys run --conversation=5af43360-c5c2-4331-98e9-7a65630a90fb
```

### What happens when you hit a Rate Limit?
1. `agys` catches the 429 / `RESOURCE_EXHAUSTED` error in real-time.
2. Puts the exhausted account into a 30-minute cooldown.
3. Automatically rotates tokens to the next healthy account.
4. Auto-resumes your conversation with `--continue` — **your coding session continues without losing state or context!**

### Dynamic Tab Title Sync
When running `agys run`, your terminal tab title automatically reflects the conversation's real topic name (e.g., `Building MCP Server`) instead of generic command strings. Compatible with CMUX, Ghostty, iTerm2, and macOS Terminal.

### Easy Resume Hints
When your session ends, `agys run` prints a quick copy-paste command to pick up right where you left off:
```text
To continue this conversation with auto-failover, run:
  agys run --conversation=5af43360-c5c2-4331-98e9-7a65630a90fb
```

---

## 🛡️ Architecture & Security

- **Atomic Token Swapping**: Writes tokens to temporary files before atomic rename, preventing file corruption if interrupted.
- **Keychain Synchronization**: Encodes tokens and syncs with macOS Keychain (`security add-generic-password -s gemini -a antigravity`) so both Antigravity CLI and IDE stay synchronized.
- **Zero Secrets in Repository**: Google OAuth client credentials are obfuscated and resolved at runtime.
- **Local Storage (`0600`)**: Profiles are stored in `~/.gemini/antigravity-cli/profiles/` accessible only by your user account.

---

## 📂 Project Structure

```text
.
├── .github/
│   └── workflows/
│       ├── ci.yml              # Automated cross-platform tests (macOS & Ubuntu)
│       └── release.yml         # Automated cross-platform release builder
├── main.go                     # CLI entrypoint, router, and command handlers
├── conversations.go            # Interactive conversation picker & SQLite metadata parser
├── conversations_test.go       # Tests for relative time & string truncation
├── profiles.go                 # Profile storage, JWT decode, atomic swap & Keychain sync
├── quota.go                    # Parallel quota inspection & 5h vs Weekly Cap optimizer
├── quota_test.go               # Tests for 5h vs Weekly reset formatting & candidate scoring
├── runner.go                   # Smart wrapper: 429 auto-failover, tab title sync & hints
├── runner_test.go              # Tests for regex UUID parsing & argument extraction
├── updater.go                  # Self-updater (agys update), version checker & release puller
├── failover.go                 # Real-time rate-limit matcher, debounce & auto-failover engine
├── install.sh                  # 1-line curl installer & local installer
├── Makefile                    # make build, make install, make clean
└── README.md                   # Documentation
```

---

## 📄 License

MIT License. Feel free to use, modify, and distribute.
