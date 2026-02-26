package ssh

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type ResolvedConfig struct {
	HostName     string
	User         string
	Port         string
	IdentityFile []string
	ProxyJump    string
	ProxyCommand string
}

func Resolve(host string) (*ResolvedConfig, error) {
	// Parse user@host:port format into separate ssh -G arguments.
	// ssh -G doesn't understand colon-delimited ports.
	var args []string
	sshHost := host
	if at := strings.Index(host, "@"); at >= 0 {
		args = append(args, "-l", host[:at])
		sshHost = host[at+1:]
	}
	if h, p, err := net.SplitHostPort(sshHost); err == nil {
		args = append(args, "-p", p)
		sshHost = h
	}
	args = append(args, "-G", sshHost)
	cmd := exec.Command("ssh", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ssh %s: %w", strings.Join(args, " "), err)
	}

	cfg := &ResolvedConfig{Port: "22"}
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), " ", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := parts[0], parts[1]
		switch key {
		case "hostname":
			cfg.HostName = val
		case "user":
			cfg.User = val
		case "port":
			cfg.Port = val
		case "identityfile":
			cfg.IdentityFile = append(cfg.IdentityFile, val)
		case "proxyjump":
			if val != "none" {
				cfg.ProxyJump = val
			}
		case "proxycommand":
			if val != "none" {
				cfg.ProxyCommand = val
			}
		}
	}
	return cfg, nil
}

type ConnectOptions struct {
	Host               string
	PasswordCallback   func() (string, error)
	PassphraseCallback func(file string) (string, error)
}

type Connection struct {
	Client        *ssh.Client
	Config        *ResolvedConfig
	host          string
	keepaliveStop chan struct{}
}

func Connect(opts ConnectOptions) (*Connection, error) {
	cfg, err := Resolve(opts.Host)
	if err != nil {
		return nil, err
	}

	authMethods := buildAuthMethods(cfg, opts)

	sshConfig := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	addr := net.JoinHostPort(cfg.HostName, cfg.Port)

	var client *ssh.Client

	if cfg.ProxyJump != "" {
		client, err = connectViaProxyJump(cfg.ProxyJump, addr, sshConfig)
	} else if cfg.ProxyCommand != "" {
		client, err = connectViaProxyCommand(cfg.ProxyCommand, addr, sshConfig)
	} else {
		client, err = ssh.Dial("tcp", addr, sshConfig)
	}
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", opts.Host, err)
	}

	conn := &Connection{
		Client:        client,
		Config:        cfg,
		host:          opts.Host,
		keepaliveStop: make(chan struct{}),
	}
	go conn.keepalive()
	return conn, nil
}

func (c *Connection) Close() error {
	close(c.keepaliveStop)
	return c.Client.Close()
}

// Host returns the original host alias used to create this connection.
func (c *Connection) Host() string {
	return c.host
}

// IsAlive checks whether the SSH connection is still responsive by sending a
// keepalive request.
func (c *Connection) IsAlive() bool {
	if c.Client == nil {
		return false
	}
	_, _, err := c.Client.SendRequest("keepalive@openssh.com", true, nil)
	return err == nil
}

func (c *Connection) keepalive() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_, _, err := c.Client.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				return
			}
		case <-c.keepaliveStop:
			return
		}
	}
}

func buildAuthMethods(cfg *ResolvedConfig, opts ConnectOptions) []ssh.AuthMethod {
	var methods []ssh.AuthMethod

	// Collect all signers into a single PublicKeys method.
	// Go's x/crypto/ssh treats each AuthMethod as a separate "attempt" for the
	// publickey method. If agent keys and file keys are separate AuthMethods,
	// the server may reject the agent key and then refuse further publickey
	// attempts (MaxAuthTries). Combining them into one method lets the library
	// try all keys in a single publickey auth exchange.
	var signers []ssh.Signer

	// Agent keys
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			if agentSigners, err := agent.NewClient(conn).Signers(); err == nil {
				signers = append(signers, agentSigners...)
			}
		}
	}

	// Identity file keys
	for _, keyPath := range cfg.IdentityFile {
		expanded := expandPath(keyPath)
		key, err := os.ReadFile(expanded)
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			// Might be encrypted — try passphrase
			if opts.PassphraseCallback != nil {
				if passphrase, cbErr := opts.PassphraseCallback(expanded); cbErr == nil {
					if signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(passphrase)); err == nil {
						signers = append(signers, signer)
					}
				}
			}
			continue
		}
		signers = append(signers, signer)
	}

	if len(signers) > 0 {
		methods = append(methods, ssh.PublicKeys(signers...))
	}

	// Password auth as fallback
	if opts.PasswordCallback != nil {
		methods = append(methods, ssh.PasswordCallback(func() (string, error) {
			return opts.PasswordCallback()
		}))
	}

	return methods
}

func connectViaProxyJump(jumpHost, target string, config *ssh.ClientConfig) (*ssh.Client, error) {
	jumpCfg, err := Resolve(jumpHost)
	if err != nil {
		return nil, err
	}
	jumpAddr := net.JoinHostPort(jumpCfg.HostName, jumpCfg.Port)

	jumpConfig := &ssh.ClientConfig{
		User:            jumpCfg.User,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			jumpConfig.Auth = []ssh.AuthMethod{ssh.PublicKeysCallback(agent.NewClient(conn).Signers)}
		}
	}

	jumpClient, err := ssh.Dial("tcp", jumpAddr, jumpConfig)
	if err != nil {
		return nil, fmt.Errorf("connect to jump host %s: %w", jumpHost, err)
	}

	conn, err := jumpClient.Dial("tcp", target)
	if err != nil {
		jumpClient.Close()
		return nil, fmt.Errorf("tunnel through %s to %s: %w", jumpHost, target, err)
	}

	ncc, chans, reqs, err := ssh.NewClientConn(conn, target, config)
	if err != nil {
		conn.Close()
		jumpClient.Close()
		return nil, err
	}
	return ssh.NewClient(ncc, chans, reqs), nil
}

func connectViaProxyCommand(proxyCmd, target string, config *ssh.ClientConfig) (*ssh.Client, error) {
	cmd := exec.Command("sh", "-c", proxyCmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("proxy command: %w", err)
	}

	rwc := &proxyConn{stdout, stdin}
	c, chans, reqs, err := ssh.NewClientConn(rwc, target, config)
	if err != nil {
		cmd.Process.Kill()
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// proxyConn wraps stdin/stdout pipes from a proxy command as a net.Conn.
type proxyConn struct {
	r interface{ Read([]byte) (int, error) }
	w interface {
		Write([]byte) (int, error)
		Close() error
	}
}

func (c *proxyConn) Read(p []byte) (int, error)         { return c.r.Read(p) }
func (c *proxyConn) Write(p []byte) (int, error)        { return c.w.Write(p) }
func (c *proxyConn) Close() error                       { return c.w.Close() }
func (c *proxyConn) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (c *proxyConn) RemoteAddr() net.Addr               { return &net.TCPAddr{} }
func (c *proxyConn) SetDeadline(_ time.Time) error      { return nil }
func (c *proxyConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *proxyConn) SetWriteDeadline(_ time.Time) error { return nil }

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
