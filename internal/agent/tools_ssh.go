package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// sshHostConfig is the per-host config stored in the ssh_hosts DB key.
type sshHostConfig struct {
	Host string `json:"host"`
	User string `json:"user"`
	Key  string `json:"key"`
}

// sshHostsConfig is the top-level JSON stored under config key "ssh_hosts".
type sshHostsConfig map[string]sshHostConfig

// loadSSHHosts reads the ssh_hosts config from DB and unmarshals it.
func (t *turn) loadSSHHosts() (sshHostsConfig, error) {
	raw := t.a.db.GetConfig(context.Background(), "ssh_hosts")
	if raw == "" {
		return nil, fmt.Errorf("ssh_hosts config is empty — set it in Settings → Configuration")
	}
	var hosts sshHostsConfig
	if err := json.Unmarshal([]byte(raw), &hosts); err != nil {
		return nil, fmt.Errorf("ssh_hosts is invalid JSON: %w", err)
	}
	return hosts, nil
}

// getSSHClient creates an SSH client for a named host config entry.
func getSSHClient(cfg sshHostConfig, timeout time.Duration) (*ssh.Client, error) {
	signer, err := ssh.ParsePrivateKey([]byte(cfg.Key))
	if err != nil {
		return nil, fmt.Errorf("ssh key parse failed: %w", err)
	}
	sshCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}
	addr := net.JoinHostPort(cfg.Host, "22")
	client, err := ssh.Dial("tcp", addr, sshCfg)
	if err != nil {
		return nil, fmt.Errorf("ssh connect to %s: %w", addr, err)
	}
	return client, nil
}

// sshExec runs a command on the named host and returns stdout+stderr+exit code.
func (t *turn) sshExec(ctx context.Context, host, cmd string, timeoutSecs int) string {
	hosts, err := t.loadSSHHosts()
	if err != nil {
		return "ERROR: " + err.Error()
	}
	cfg, ok := hosts[host]
	if !ok {
		return fmt.Sprintf("ERROR: unknown ssh host %q — available: %s", host, listSSHHostKeys(hosts))
	}
	if timeoutSecs <= 0 {
		timeoutSecs = 30
	}
	timeout := time.Duration(timeoutSecs) * time.Second

	client, err := getSSHClient(cfg, 10*time.Second)
	if err != nil {
		return "ERROR: " + err.Error()
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "ERROR: ssh session: " + err.Error()
	}
	defer session.Close()

	var stdout, stderr strings.Builder
	session.Stdout = &stdout
	session.Stderr = &stderr

	resultCh := make(chan error, 1)
	go func() {
		resultCh <- session.Run(cmd)
	}()

	select {
	case runErr := <-resultCh:
		out := stdout.String()
		if stderr.Len() > 0 {
			if out != "" {
				out += "\n"
			}
			out += "STDERR:\n" + stderr.String()
		}
		if runErr != nil {
			if exitErr, ok := runErr.(*ssh.ExitError); ok {
				return fmt.Sprintf("%s\nEXIT: %d", truncate(out, 8000), exitErr.ExitStatus())
			}
			return fmt.Sprintf("ERROR: %v\n%s", runErr, truncate(out, 8000))
		}
		return truncate(out, 8000)
	case <-time.After(timeout):
		return fmt.Sprintf("ERROR: ssh command timed out after %ds", timeoutSecs)
	case <-ctx.Done():
		return "ERROR: cancelled"
	}
}

// sshUpload writes content to a file on a remote host.
func (t *turn) sshUpload(ctx context.Context, host, localContent, remotePath string) string {
	hosts, err := t.loadSSHHosts()
	if err != nil {
		return "ERROR: " + err.Error()
	}
	cfg, ok := hosts[host]
	if !ok {
		return fmt.Sprintf("ERROR: unknown ssh host %q — available: %s", host, listSSHHostKeys(hosts))
	}

	client, err := getSSHClient(cfg, 10*time.Second)
	if err != nil {
		return "ERROR: " + err.Error()
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "ERROR: ssh session: " + err.Error()
	}
	defer session.Close()

	// Pipe content via stdin to a remote shell command that writes the file.
	session.Stdin = strings.NewReader(localContent)

	var stderr strings.Builder
	session.Stderr = &stderr

	// Use cat with a heredoc-like approach — safer than echo for arbitrary content.
	escaped := strings.ReplaceAll(localContent, "'", "'\\''")
	cmd := fmt.Sprintf("cat > %s << 'KAPTAAN_EOF'\n%s\nKAPTAAN_EOF", shellQuote(remotePath), escaped)

	if err := session.Run(cmd); err != nil {
		if exitErr, ok := err.(*ssh.ExitError); ok {
			return fmt.Sprintf("ERROR: exit %d: %s", exitErr.ExitStatus(), stderr.String())
		}
		return "ERROR: " + err.Error()
	}
	return fmt.Sprintf("uploaded %d bytes → %s on %s", len(localContent), remotePath, host)
}

// sshRead reads a file from a remote host.
func (t *turn) sshRead(ctx context.Context, host, remotePath string) string {
	hosts, err := t.loadSSHHosts()
	if err != nil {
		return "ERROR: " + err.Error()
	}
	cfg, ok := hosts[host]
	if !ok {
		return fmt.Sprintf("ERROR: unknown ssh host %q — available: %s", host, listSSHHostKeys(hosts))
	}

	client, err := getSSHClient(cfg, 10*time.Second)
	if err != nil {
		return "ERROR: " + err.Error()
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "ERROR: ssh session: " + err.Error()
	}
	defer session.Close()

	var stdout, stderr strings.Builder
	session.Stdout = &stdout
	session.Stderr = &stderr

	if err := session.Run("cat " + shellQuote(remotePath)); err != nil {
		if exitErr, ok := err.(*ssh.ExitError); ok {
			return fmt.Sprintf("ERROR: exit %d: %s", exitErr.ExitStatus(), stderr.String())
		}
		return "ERROR: " + err.Error()
	}
	return truncate(stdout.String(), 8000)
}

func listSSHHostKeys(hosts sshHostsConfig) string {
	keys := make([]string, 0, len(hosts))
	for k := range hosts {
		keys = append(keys, k)
	}
	return strings.Join(keys, ", ")
}
