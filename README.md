# 🚀 agy-switch

> **A blazing-fast, lightweight, and secure multi-account manager for Google Antigravity CLI (`agy`).**

Built in **Golang (Go Standard Library)** and compiled into a standalone native binary with zero runtime dependencies. It enables instant 1-second switching between Google accounts to bypass rate limits or separate work and personal profiles.

---

## 🌟 Key Features

* ⚡ **Blazing Fast**: Native compiled Go binary running with near-zero latency and minimal footprint.
* 🎯 **Smart Interactive Menu**: Simply run `agy-switch` without arguments to view and select profiles via numbers `[1]`, `[2]`, etc.
* 🔍 **Automatic Account Detection**: Decodes OpenID Connect JWT tokens to display Google account email, display name, and session validity.
* 🔒 **Strict Security (`0600`)**: Tokens are stored locally in `~/.gemini/antigravity-cli/profiles/` with strict `0600` permissions (user read/write only). 100% offline, zero network requests to third parties.
* 🔄 **CLI Refresh Compatible**: Reliably identifies active profiles even when `agy` automatically refreshes the short-lived `access_token` in the background.

---

## 📖 Command Reference

| Command | Alias / Short | Description |
| :--- | :--- | :--- |
| `agy-switch` | | Open interactive selection menu |
| `agy-switch <profile>` | `agy-switch switch <profile>` | Instantly switch to `<profile>` |
| `agy-switch list` | `agy-switch ls` | List all saved profiles and token statuses |
| `agy-switch current` | `whoami`, `status` | View currently active account details |
| `agy-switch save [name]` | `add` | Save current active session as a named profile |
| `agy-switch login [name]` | | Interactive wizard to log in to a new account and save profile |
| `agy-switch remove <name>` | `rm`, `delete` | Delete a saved profile |
| `agy-switch install` | | Link binary globally to `/opt/homebrew/bin/agy-switch` |
| `agy-switch help` | `-h`, `--help` | Display command help |

---

## 🚀 Quick Start Guide

### 1. Save Your Current Account
Save your currently active account as your primary profile:
```bash
agy-switch save main
```

### 2. Add a Second Account (e.g., `work` or `acc2`)
Use the built-in login helper:
```bash
agy-switch login work
```
*The wizard temporarily backs up your active session, launches `agy` to complete Google authentication in your browser, and saves the new token as `work` when you type `/exit`.*

> **Manual Alternative:**
> 1. Temporarily clear active token: `rm ~/.gemini/antigravity-cli/antigravity-oauth-token`
> 2. Run `agy` and complete Google login in your browser, then type `/exit`.
> 3. Save as profile: `agy-switch save work`

### 3. Switch Between Accounts
Choose whichever method fits your workflow best:

* **Direct Switch:**
  ```bash
  agy-switch work
  # or switch back
  agy-switch main
  ```

* **Interactive Menu:**
  ```bash
  agy-switch
  ```
  ```text
  [ 1] main             personal@gmail.com         ● ACTIVE         🟢 Valid (Auto-refresh)
  [ 2] work             work@company.com           ○ Ready          🟢 Valid (Auto-refresh)

  👉 Enter number: 2
  ```

* **Inspect Active Profile:**
  ```bash
  agy-switch current
  ```

---

## 🛠️ Build from Source

To customize or recompile from source:

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
├── main.go         # Golang source code
├── agy-switch      # Native compiled arm64 binary
└── README.md       # Documentation
```
