package sshutil

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type Client struct {
	Host       string
	Port       int
	User       string
	KeyPath    string
	Password   string
	Timeout    time.Duration
	sshClient  *ssh.Client
	sftpClient *sftp.Client
}

type Options struct {
	Host     string
	Port     int
	User     string
	KeyPath  string
	Password string
	Timeout  time.Duration
}

func NewClient(opts Options) *Client {
	if opts.Port <= 0 {
		opts.Port = 22
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	return &Client{
		Host:     opts.Host,
		Port:     opts.Port,
		User:     opts.User,
		KeyPath:  opts.KeyPath,
		Password: opts.Password,
		Timeout:  opts.Timeout,
	}
}

// Connect establishes the SSH and SFTP connection
func (c *Client) Connect() error {
	var authMethods []ssh.AuthMethod

	if c.KeyPath != "" {
		expandedKey := c.KeyPath
		if strings.HasPrefix(expandedKey, "~/") {
			home, _ := os.UserHomeDir()
			expandedKey = filepath.Join(home, expandedKey[2:])
		}
		keyBytes, err := os.ReadFile(expandedKey)
		if err == nil {
			signer, err := ssh.ParsePrivateKey(keyBytes)
			if err == nil {
				authMethods = append(authMethods, ssh.PublicKeys(signer))
			}
		}
	}

	if c.Password != "" {
		authMethods = append(authMethods, ssh.Password(c.Password))
	}

	if len(authMethods) == 0 {
		return errors.New("no valid authentication method provided (check key path or password)")
	}

	sshConfig := &ssh.ClientConfig{
		User:            c.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // Suitable for private LAN NAS/Ubuntu setup
		Timeout:         c.Timeout,
	}

	addr := net.JoinHostPort(c.Host, fmt.Sprintf("%d", c.Port))
	client, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return fmt.Errorf("ssh connection failed to %s: %w", addr, err)
	}
	c.sshClient = client

	sftpCli, err := sftp.NewClient(client)
	if err != nil {
		client.Close()
		return fmt.Errorf("sftp subsystem failed: %w", err)
	}
	c.sftpClient = sftpCli

	return nil
}

// Close closes the SSH and SFTP connections
func (c *Client) Close() error {
	var errs []string
	if c.sftpClient != nil {
		if err := c.sftpClient.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if c.sshClient != nil {
		if err := c.sshClient.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// Run executes a remote command and returns stdout + stderr
func (c *Client) Run(cmd string) (string, error) {
	if c.sshClient == nil {
		return "", errors.New("not connected to remote host")
	}

	session, err := c.sshClient.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create ssh session: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	err = session.Run(cmd)
	combined := stdout.String()
	if stderr.Len() > 0 {
		if combined != "" {
			combined += "\n"
		}
		combined += stderr.String()
	}

	return combined, err
}

// UploadContent writes content directly to a remote path via SFTP
func (c *Client) UploadContent(remotePath string, content []byte, mode os.FileMode) error {
	if c.sftpClient == nil {
		return errors.New("SFTP not connected")
	}

	dir := filepath.Dir(remotePath)
	_ = c.sftpClient.MkdirAll(dir)

	file, err := c.sftpClient.OpenFile(remotePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC)
	if err != nil {
		return fmt.Errorf("failed to open remote file %s: %w", remotePath, err)
	}
	defer file.Close()

	if _, err := file.Write(content); err != nil {
		return fmt.Errorf("failed to write to remote file %s: %w", remotePath, err)
	}

	return c.sftpClient.Chmod(remotePath, mode)
}

// DownloadContent reads a remote file's content
func (c *Client) DownloadContent(remotePath string) ([]byte, error) {
	if c.sftpClient == nil {
		return nil, errors.New("SFTP not connected")
	}

	file, err := c.sftpClient.Open(remotePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open remote file %s: %w", remotePath, err)
	}
	defer file.Close()

	return io.ReadAll(file)
}

// ServiceStatus queries systemctl status for a service
func (c *Client) ServiceStatus(serviceName string) (string, bool, error) {
	out, err := c.Run(fmt.Sprintf("systemctl is-active %s", serviceName))
	status := strings.TrimSpace(out)
	isActive := (status == "active")
	return status, isActive, err
}

// RestartService restarts the systemd service on Ubuntu
func (c *Client) RestartService(serviceName string) (string, error) {
	return c.Run(fmt.Sprintf("sudo systemctl restart %s", serviceName))
}

// ServiceLogs retrieves the most recent journal logs for a service
func (c *Client) ServiceLogs(serviceName string, lines int) (string, error) {
	if lines <= 0 {
		lines = 50
	}
	return c.Run(fmt.Sprintf("journalctl -u %s -n %d --no-pager", serviceName, lines))
}
