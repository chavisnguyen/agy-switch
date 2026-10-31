# 🚀 agys

<p align="center">
  <b>A blazing-fast, lightweight, and zero-dependency multi-account manager & auto-failover runner for Google Antigravity CLI (<code>agy</code>).</b>
</p>

<p align="center">
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

| Command | Description |
| :--- | :--- |
| `agys` | Interactive numbered menu to select and switch accounts |
| `agys <profile>` | Instantly switch to `<profile>` |
| `agys next` | Rotate to the next available healthy profile (Round-Robin) |
| `agys status` | View active account details and real-time Claude/Gemini quotas |
| `agys list` | Formatted table of all saved profiles and quota percentages |
| `agys save [name]` | Save current active session as a named profile |
| `agys login [name]` | Interactive wizard to authenticate a new Google account |
| `agys rename <old> <new>` | Rename an existing saved profile |
| `agys remove <name>` | Delete a saved profile |
| **`agys run [args...]`** | **Smart runner**: runs `agy` with auto-failover, resume hints & tab sync |
| **`agys watch start`** | **Background daemon**: monitors 429 errors and auto-switches tokens |
| `agys watch stop` | Stop the background watcher daemon |
| `agys watch status` | Check status and PID of the watcher daemon |
| `agys cooldown` | View profiles currently in cooldown penalty |
| `agys cooldown clear` | Clear all cooldown penalties immediately |
| **`agys update`** | **Self-update**: automatically downloads and updates `agys` to latest release |
| `agys version` | Show current installed version & architecture |
| `agys help` | Display built-in help |

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

### 3. Check Real-Time Quotas
```bash
agys status
```
Output preview:
```text
📊 AGY ACCOUNTS STATUS & QUOTA
══════════════════════════════════════════════════════════
[1] work (● ACTIVE)
    Email  : work.developer@gmail.com
    Claude : 🟢 100.0% (resets 31 Oct 22:54)
    Gemini : 🟢  76.1% (resets 31 Oct 20:13)
    ──────────────────────────────────────────────────
[2] main (○ Ready)
    Email  : personal@gmail.com
    Claude : 🔴   0.0% (resets 02 Nov 21:20)
    Gemini : 🟡   0.1% (resets 31 Oct 21:00)
    ──────────────────────────────────────────────────
[3] personal2 (○ Ready)
    Email  : personal2@gmail.com
    Claude : 🔴   0.0% (resets 05 Nov 14:39)
    Gemini : 🔴   0.0% (resets 04 Nov 01:12)
══════════════════════════════════════════════════════════
```

### 4. Switch Accounts On The Fly
```bash
# Switch directly
agys main

# Or rotate to next healthy account
agys next

# Or open interactive menu
agys
```

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
│       └── release.yml  # Automated cross-platform release builder
├── main.go              # CLI router, list, and interactive commands
├── profiles.go          # Profile storage, JWT decode, atomic swap & Keychain sync
├── quota.go             # Parallel real-time quota inspection (Claude & Gemini)
├── runner.go            # Smart wrapper: 429 auto-failover, tab title sync & hints
├── updater.go           # Self-updater (agys update), version checker & release puller
├── watcher.go           # Log tailer, regex detector, background daemon & macOS alerts
├── install.sh           # 1-line curl installer & local installer
├── Makefile             # make build, make install, make clean
└── README.md            # Documentation
```

---

## 📄 License

MIT License. Feel free to use, modify, and distribute.
