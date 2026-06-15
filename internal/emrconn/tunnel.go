package emrconn

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ryandam9/aws_explorer/internal/config"
)

// tunnelDialer reaches on-cluster daemons by opening an SSH connection to the
// target host and dialing the daemon through it (the "tunnel" mode). SSH clients
// are established lazily per host and cached, so repeated requests to the same
// cluster reuse one connection.
type tunnelDialer struct {
	cfg     *ssh.ClientConfig
	sshPort int

	mu      sync.Mutex
	clients map[string]*ssh.Client
}

// newTunnelDialer builds a tunnelDialer from the SSH settings. It loads the
// private key up front so a bad key fails at construction, not mid-browse.
func newTunnelDialer(s config.OnClusterSSH, timeout time.Duration) (*tunnelDialer, error) {
	if s.User == "" {
		return nil, fmt.Errorf("tunnel mode requires emr.onCluster.ssh.user (e.g. hadoop)")
	}
	if s.KeyFile == "" {
		return nil, fmt.Errorf("tunnel mode requires emr.onCluster.ssh.keyFile")
	}
	signer, err := loadPrivateKey(s.KeyFile)
	if err != nil {
		return nil, err
	}
	port := s.Port
	if port == 0 {
		port = 22
	}
	return &tunnelDialer{
		cfg: &ssh.ClientConfig{
			User:    s.User,
			Auth:    []ssh.AuthMethod{ssh.PublicKeys(signer)},
			Timeout: timeout,
			// EMR primary nodes are ephemeral and not in known_hosts, so host-key
			// pinning isn't practical here; this is documented as a caveat of the
			// opt-in tunnel mode.
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		},
		sshPort: port,
		clients: map[string]*ssh.Client{},
	}, nil
}

// dialContext implements the http.Transport DialContext hook: it SSHes to the
// target host (the address's host) and dials the daemon port through the SSH
// session via the remote's loopback.
func (t *tunnelDialer) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	client, err := t.clientFor(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("%w: ssh to %s: %v", ErrUnreachable, host, err)
	}
	// The daemon listens on the primary node itself; dial it from there.
	conn, err := client.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		return nil, fmt.Errorf("%w: tunnel dial 127.0.0.1:%s: %v", ErrUnreachable, port, err)
	}
	return conn, nil
}

// clientFor returns a cached SSH client for host, dialing a new one if needed.
func (t *tunnelDialer) clientFor(ctx context.Context, host string) (*ssh.Client, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if c, ok := t.clients[host]; ok {
		return c, nil
	}
	d := net.Dialer{Timeout: t.cfg.Timeout}
	raw, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, fmt.Sprintf("%d", t.sshPort)))
	if err != nil {
		return nil, err
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(raw, host, t.cfg)
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	t.clients[host] = client
	return client, nil
}

// Close tears down all cached SSH clients (called when the dialer is done).
func (t *tunnelDialer) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, c := range t.clients {
		_ = c.Close()
	}
	t.clients = map[string]*ssh.Client{}
}

// loadPrivateKey reads and parses an unencrypted SSH private key, expanding a
// leading "~".
func loadPrivateKey(path string) (ssh.Signer, error) {
	path = expandHome(path)
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read ssh key %q: %w", path, err)
	}
	signer, err := ssh.ParsePrivateKey(pem)
	if err != nil {
		return nil, fmt.Errorf("parse ssh key %q (passphrase-protected keys are not supported): %w", path, err)
	}
	return signer, nil
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
