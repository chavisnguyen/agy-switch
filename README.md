# 🚀 agys (agy-switch)

> **A blazing-fast, lightweight, and secure multi-account manager with automatic rate-limit failover for Google Antigravity CLI (`agy`).**

Written in **Golang (Go Standard Library)** and compiled into a standalone native binary with zero runtime dependencies. It enables instant 1-second switching between Google accounts and provides **automatic failover** when hitting HTTP 429 / Quota limits.

---

## 📖 Command Reference

| Command | Description |
| :--- | :--- |
| `agys` | Open interactive selection menu |
| `agys <profile>` | Instantly switch to `<profile>` |
| `agys next` | Rotate to the next available account (Round-Robin) |
| `agys list` | List all saved profiles with real-time Claude & Gemini quotas |
| `agys status` | View currently active account details and real-time quota |
| `agys save [name]` | Save current active session as a named profile |
| `agys login [name]` | Interactive wizard to log in to a new account and save profile |
| `agys rename <old> <new>` | Rename an existing saved profile |
| `agys remove <name>` | Delete a saved profile |
| **`agys run [args...]`** | **Smart wrapper**: runs `agy` with auto-switch & `--continue` resume |
| **`agys watch start`** | **Start background daemon** to auto-switch on 429 & send macOS alerts |
| `agys watch stop` | Stop the background watcher daemon |
| `agys watch status` | Check status of the watcher daemon |
| `agys cooldown` | View profiles currently in cooldown |
| `agys cooldown clear` | Clear all cooldown penalties immediately |
| `agys install` | Link binary globally to `/opt/homebrew/bin/agys` |

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

## 🛠️ Quick Installation

```bash
cd /Users/chavis/Documents/personal/tool/agy-switch
make install
source ~/.zshrc
```
* Builds native binary with optimizations (`-ldflags="-s -w"`).
* Links `/opt/homebrew/bin/agy-switch` globally.
* Adds `alias agy="agy-switch run"` to `~/.zshrc` automatically for seamless auto-failover.

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
