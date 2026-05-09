# Kaptaan

An autonomous coding agent you host yourself. Give it a task in plain English — it plans, writes code, runs shell commands in an E2B sandbox, and commits back to your GitHub repo. All configuration is managed through the web UI; no environment variables or config files required.

---

## How it works

- You chat with the agent through a web UI
- The agent uses **DeepSeek V4 Pro** to reason and plan
- Code execution and git operations happen inside an **E2B** sandbox (isolated, ephemeral)
- Conversation history, memories, and configuration are persisted in a local **SQLite** database
- Auth is session-based with bcrypt-hashed passwords

---

## Self-hosting on AWS Lightsail

### Prerequisites

- A Lightsail instance running **Ubuntu 22.04 or 24.04** (any size; 1 GB RAM is sufficient)
- The instance's **browser-based SSH console** (or any SSH access)
- Port **5000** open in the Lightsail instance firewall
- A **GitHub Personal Access Token (PAT)** with `repo` (read) scope — needed because this is a private repository

### Step 1 — Open port 5000 in Lightsail

In the Lightsail console go to your instance → **Networking** tab → **Add rule**:

- Application: `Custom`
- Protocol: `TCP`
- Port: `5000`

### Step 2 — Run the installer

Open the browser-based SSH console and run the two commands below.  
Replace `ghp_YOUR_TOKEN_HERE` with your actual GitHub PAT both times.

```bash
curl -fsSL \
  -H "Authorization: token ghp_YOUR_TOKEN_HERE" \
  https://raw.githubusercontent.com/sumit-waani/kaptaan/main/install.sh \
  -o /tmp/install.sh
```
```bash
sudo REPO_URL="https://ghp_YOUR_TOKEN_HERE@github.com/sumit-waani/kaptaan" bash /tmp/install.sh
```


That's it. The script will:

1. Install Go (if missing or outdated)
2. Clone the repository to `/opt/kaptaan`
3. Build the binary and install it to `/usr/local/bin/kaptaan`
4. Create `/var/lib/kaptaan/` as the persistent data directory (database lives here)
5. Register and start a **systemd service** (`kaptaan.service`) that auto-restarts on crash and survives reboots

> **Your data is safe on updates.** The database at `/var/lib/kaptaan/kaptaan.db` is never deleted or modified by the installer. The schema migrations inside the app itself use `CREATE TABLE IF NOT EXISTS` — safe to run any number of times.

> **Token security.** The PAT is only used in memory during clone/pull and is immediately stripped from the stored git remote URL — it is never written to disk or the systemd service file.

### Step 3 — Open the web UI

Navigate to `http://<your-lightsail-ip>:5000` in your browser.

On first visit you will be prompted to create an account (username + password). After logging in, go to **Settings → Configuration** and fill in:

| Key | Description |
|---|---|
| `deepseek_api_key` | Your DeepSeek API key (from [platform.deepseek.com](https://platform.deepseek.com)) |
| `deepseek_model` | Model name — leave blank to use `deepseek-v4-pro` |
| `e2b_api_key` | Your E2B API key (from [e2b.dev](https://e2b.dev)) |
| `repo_url` | GitHub repo URL the agent will work on |
| `github_token` | GitHub personal access token with repo write access |

All keys are stored in the local SQLite database — never in environment variables or config files.

---

## Updating to a new version

SSH into the instance and run the same two commands again with your PAT:

```bash
curl -fsSL \
  -H "Authorization: token ghp_YOUR_TOKEN_HERE" \
  https://raw.githubusercontent.com/cto-agent/cto-agent/main/install.sh \
  -o /tmp/install.sh

sudo GITHUB_TOKEN=ghp_YOUR_TOKEN_HERE bash /tmp/install.sh
```

It will pull the latest code, rebuild the binary, and restart the service. **Your database and all configuration are untouched.**

---

## Useful commands on the server

```bash
# Check service status
sudo systemctl status kaptaan

# Follow live logs
sudo journalctl -u kaptaan -f

# Restart the service
sudo systemctl restart kaptaan

# Stop the service
sudo systemctl stop kaptaan
```

The database file is at `/var/lib/kaptaan/kaptaan.db`.

---

## Tech stack

| Layer | Technology |
|---|---|
| Language | Go (standard library) |
| Database | SQLite (`modernc.org/sqlite` — pure Go, no CGO) |
| Frontend | Vanilla HTML/CSS/JS + Alpine.js (embedded in the binary) |
| LLM | DeepSeek API |
| Sandbox | E2B |
| Auth | Session cookies + bcrypt |
