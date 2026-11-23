// Package sshx executes scripts on remote Linux (bash) and Windows (PowerShell)
// hosts over SSH, streaming output line by line.
package sshx

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
	"unicode/utf16"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/Songxwn/Auto-Deploy/internal/models"
)

// TestScript returns a trivial script used for connection tests.
func TestScript(osType string) string {
	if osType == models.OSWindows {
		return "Write-Output 'auto-deploy-ok'"
	}
	return "echo auto-deploy-ok"
}

// EncodeCommand encodes a PowerShell script as UTF-16LE base64 for -EncodedCommand.
func EncodeCommand(script string) string {
	u16 := utf16.Encode([]rune(script))
	buf := make([]byte, 2*len(u16))
	for i, r := range u16 {
		binary.LittleEndian.PutUint16(buf[i*2:], r)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// wrapWindows makes PowerShell fail fast and propagate native exit codes.
func wrapWindows(script string) string {
	return "$ErrorActionPreference = 'Stop'\n" +
		"try {\n" + script + "\n} catch {\n" +
		"  Write-Output ('ERROR: ' + $_.Exception.Message)\n" +
		"  exit 1\n" +
		"}\n" +
		"if ($null -ne $LASTEXITCODE -and $LASTEXITCODE -ne 0) { exit $LASTEXITCODE }\n"
}

func clientConfig(h models.Host, secret, passphrase string, timeout time.Duration, knownHostsPath string) (*ssh.ClientConfig, error) {
	var auth []ssh.AuthMethod
	switch h.AuthType {
	case models.AuthPassword:
		auth = append(auth,
			ssh.Password(secret),
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range questions {
					answers[i] = secret
				}
				return answers, nil
			}),
		)
	case models.AuthKey:
		var (
			signer ssh.Signer
			err    error
		)
		if passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(secret), []byte(passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(secret))
		}
		if err != nil {
			return nil, fmt.Errorf("解析私钥失败: %w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	default:
		return nil, fmt.Errorf("未知认证方式: %s", h.AuthType)
	}

	hostKey := ssh.InsecureIgnoreHostKey()
	if knownHostsPath != "" {
		cb, err := knownhosts.New(knownHostsPath)
		if err != nil {
			return nil, fmt.Errorf("加载 known_hosts 失败: %w", err)
		}
		hostKey = cb
	}
	return &ssh.ClientConfig{
		User:            h.User,
		Auth:            auth,
		HostKeyCallback: hostKey,
		Timeout:         timeout,
	}, nil
}

// Run executes script on the host and invokes onLine for every output line.
// It returns the remote exit code; a non-nil error means the command could not
// be executed or the context was canceled (exit code -1 in that case).
func Run(ctx context.Context, h models.Host, secret, passphrase, script string, dialTimeout time.Duration, knownHostsPath string, onLine func(stream, text string)) (int, error) {
	cfg, err := clientConfig(h, secret, passphrase, dialTimeout, knownHostsPath)
	if err != nil {
		return -1, err
	}
	addr := net.JoinHostPort(h.Addr, strconv.Itoa(h.Port))
	dialer := net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return -1, fmt.Errorf("连接 %s 失败: %w", addr, err)
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		return -1, fmt.Errorf("SSH 握手失败: %w", err)
	}
	client := ssh.NewClient(c, chans, reqs)
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		return -1, fmt.Errorf("创建 SSH 会话失败: %w", err)
	}
	defer sess.Close()

	var (
		cmd       string
		stdinData string
	)
	if h.OSType == models.OSWindows {
		cmd = "powershell -NoProfile -NonInteractive -EncodedCommand " + EncodeCommand(wrapWindows(script))
	} else {
		cmd = "bash -s"
		stdinData = script
	}

	stdout, err := sess.StdoutPipe()
	if err != nil {
		return -1, err
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		return -1, err
	}
	var stdin io.WriteCloser
	if stdinData != "" {
		stdin, err = sess.StdinPipe()
		if err != nil {
			return -1, err
		}
	}
	if err := sess.Start(cmd); err != nil {
		return -1, fmt.Errorf("启动远程命令失败: %w", err)
	}
	if stdin != nil {
		io.WriteString(stdin, stdinData)
		stdin.Close()
	}

	// Abort the session when the caller cancels.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			sess.Close()
			client.Close()
		case <-stop:
		}
	}()

	var wg sync.WaitGroup
	scan := func(r io.Reader, stream string) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for sc.Scan() {
			onLine(stream, sc.Text())
		}
	}
	wg.Add(2)
	go scan(stdout, "stdout")
	go scan(stderr, "stderr")
	wg.Wait()

	waitErr := sess.Wait()
	if ctx.Err() != nil {
		return -1, ctx.Err()
	}
	if waitErr != nil {
		var exitErr *ssh.ExitError
		if errors.As(waitErr, &exitErr) {
			return exitErr.ExitStatus(), nil
		}
		return -1, waitErr
	}
	return 0, nil
}
