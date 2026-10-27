# 🚀 agy-switch

> **A blazing-fast, lightweight, and secure multi-account manager with automatic rate-limit failover for Google Antigravity CLI (`agy`).**

Written in **Golang (Go Standard Library)** and compiled into a standalone native binary with zero runtime dependencies. It enables instant 1-second switching between Google accounts and provides **automatic failover** when hitting HTTP 429 / Quota limits.

---

## 🌟 Key Features

* ⚡ **Blazing Fast**: Native compiled Go binary running with near-zero latency and minimal resource footprint.
* 🔄 **Automatic Rate-Limit Failover**:
  * **Smart Wrapper (`agy-switch run`)**: Runs `agy` and automatically swaps accounts and resumes your conversation (`--continue`) when a 429 / Quota limit is hit.
  * **Background Watcher (`agy-switch watch start`)**: Monitors `cli.log` in the background, automatically swaps tokens on quota exhaustion, and sends native **macOS notifications**.
* ⏱️ **Cooldown / Penalty System**: Temporarily cools down rate-limited accounts (30 minutes) to prevent cyclic switching into exhausted profiles.
* 🎯 **Round-Robin Rotation (`agy-switch next`)**: Instantly cycle to the next healthy account in your pool.
* 🔍 **Automatic Account Detection**: Decodes OpenID Connect JWT tokens offline to display Google account email, name, and validity.
* 🔒 **Strict Security (`0600`)**: Tokens are stored locally in `~/.gemini/antigravity-cli/profiles/` with strict `0600` permissions (user read/write only). 100% offline.

---

## 📖 Command Reference

| Command | Alias | Description |
| :--- | :--- | :--- |
| `agy-switch` | | Open interactive selection menu |
| `agy-switch <profile>` | `switch` | Instantly switch to `<profile>` |
| `agy-switch next` | | Rotate to the next available account (Round-Robin) |
| `agy-switch list` | `ls` | List all saved profiles, cooldowns, and token statuses |
| `agy-switch current` | `whoami`, `status` | View currently active account details |
| `agy-switch save [name]` | `add` | Save current active session as a named profile |
| `agy-switch login [name]` | | Interactive wizard to log in to a new account and save profile |
| `agy-switch rename <old> <new>` | `mv` | Rename an existing saved profile |
| `agy-switch remove <name>` | `rm`, `delete` | Delete a saved profile |
| **`agy-switch run [args...]`** | | **Smart wrapper**: runs `agy` with auto-switch & `--continue` resume |
| **`agy-switch watch start`** | | **Start background daemon** to auto-switch on 429 & send macOS alerts |
| `agy-switch watch stop` | | Stop the background watcher daemon |
| `agy-switch watch status` | | Check status of the watcher daemon |
| `agy-switch cooldown` | | View profiles currently in cooldown |
| `agy-switch cooldown clear` | `reset` | Clear all cooldown penalties immediately |
| `agy-switch install` | | Link binary globally to `/opt/homebrew/bin/agy-switch` |

---

## ⚡ How Auto-Switch Works

### Option 1: The Smart Runner (`agy-switch run`)
Instead of launching `agy` directly, run:
```bash
agy-switch run
```
*(Or create a shell alias: `alias agy="agy-switch run"`)*

* When `agy` encounters a Rate Limit / `RESOURCE_EXHAUSTED` error during a conversation turn:
  1. `agy-switch` detects the 429 error in real-time.
  2. Marks the exhausted account in cooldown for 30 minutes.
  3. Automatically swaps `antigravity-oauth-token` to the next healthy account in your pool.
  4. Automatically resumes your conversation with `--continue` — **your chat session continues uninterrupted without losing context!**

### Option 2: The Background Watcher Daemon (`agy-switch watch`)
If you prefer running `agy` natively in various terminal tabs or IDEs:
```bash
# Start background watcher
agy-switch watch start

# Check status
agy-switch watch status

# Stop background watcher
agy-switch watch stop
```
* The daemon quietly monitors `~/.gemini/antigravity-cli/cli.log` with virtually zero CPU/RAM overhead (~3MB).
* As soon as any session logs a quota exhaustion error, the daemon swaps tokens immediately on disk and sends a macOS desktop notification:
  > 🔔 **agy-switch: Quota Auto-Switch**  
  > *Quota limit on 'main'. Auto-switched to 'work'!*

---

## 🚀 Quick Start Guide

### 1. Save Your Current Account
```bash
agy-switch save main
```

### 2. Add More Accounts
Use the built-in login helper:
```bash
agy-switch login work
```
*The wizard temporarily stashes your active session, launches `agy` to authenticate via Google in your browser, and saves the new token as `work` when you type `/exit`.*

### 3. Switch Between Accounts
```bash
# Direct switch by name
agy-switch work

# Or rotate to the next available account
agy-switch next

# Or use the interactive menu
agy-switch
```

---

## 🛠️ Build from Source

```bash
cd /Users/chavis/Documents/personal/tool/agy-switch
go build -ldflags="-s -w" -o agy-switch .
./agy-switch install
```

---

## 📂 Project Structure

```text
/Users/chavis/Documents/personal/tool/agy-switch/
├── go.mod          # Go module file
├── main.go         # CLI router, list, and interactive commands
├── profiles.go     # Profile storage, JWT decode, atomic swap & cooldowns
├── watcher.go      # Log tailer, regex detector, daemon & macOS notifications
├── runner.go       # Smart wrapper with auto-switch & conversation resume
├── agy-switch      # Native compiled arm64 binary
└── README.md       # Documentation
```
