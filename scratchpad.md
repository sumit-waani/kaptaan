# SSH, GitHub, Cloudflare tools implementation

- [x] Create `internal/agent/tools_ssh.go` — ssh_exec, ssh_upload, ssh_read
- [x] Create `internal/agent/tools_cloudflare.go` — Cloudflare DNS + cache + analytics
- [x] Extend `internal/agent/github.go` — issues, workflows, branches, file fetch
- [x] Register all new tools in `internal/agent/tooldefs.go`
- [x] Add dispatch cases in `internal/agent/agent.go`
- [x] Update system prompt in agent.go to mention new capabilities
- [x] Add new config keys to `internal/web/config.go` (cf_api_token, cf_zone_id, ssh_hosts)
- [x] Add config UI fields in `internal/web/static/index.html`
- [x] Add config JS in `internal/web/static/app.js`
- [x] Build verification — full build OOM'd but all files pass gofmt syntax check
