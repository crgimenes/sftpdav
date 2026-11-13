package main

import (
	"context"
	"crypto/ed25519"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// sshServer serves SFTP on 127.0.0.1; drop closes every connection it
// holds, as sshd does when the session is killed.
type sshServer struct {
	addr   string
	hostPK ssh.PublicKey

	mu    sync.Mutex
	conns []net.Conn
}

func startSSHServer(t *testing.T) *sshServer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	s := &sshServer{addr: ln.Addr().String(), hostPK: signer.PublicKey()}
	t.Cleanup(s.drop)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			s.mu.Lock()
			s.conns = append(s.conns, conn)
			s.mu.Unlock()
			go serveSFTP(conn, cfg)
		}
	}()
	return s
}

func serveSFTP(conn net.Conn, cfg *ssh.ServerConfig) {
	_, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "session only")
			continue
		}
		ch, chReqs, err := nc.Accept()
		if err != nil {
			continue
		}
		go func() {
			for req := range chReqs {
				ok := req.Type == "subsystem" && string(req.Payload[4:]) == "sftp"
				_ = req.Reply(ok, nil)
				if !ok {
					continue
				}
				srv, err := sftp.NewServer(ch)
				if err != nil {
					return
				}
				_ = srv.Serve()
			}
		}()
	}
}

func (s *sshServer) drop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		_ = c.Close()
	}
	s.conns = nil
}

func TestRemoteRedialsAfterConnectionLoss(t *testing.T) {
	srv := startSSHServer(t)
	dials := 0
	r := &remote{dial: func(ctx context.Context) (*ssh.Client, error) {
		dials++
		return ssh.Dial("tcp", srv.addr, &ssh.ClientConfig{
			User:            "test",
			HostKeyCallback: ssh.FixedHostKey(srv.hostPK),
			Timeout:         5 * time.Second,
		})
	}}
	dir := t.TempDir()
	ctx := context.Background()

	first, err := r.sftp(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = first.Stat(dir)
	if err != nil {
		t.Fatalf("stat on the first connection: %v", err)
	}

	srv.drop()

	deadline := time.Now().Add(5 * time.Second)
	for {
		c, err := r.sftp(ctx)
		if err == nil && c != first {
			_, err = c.Stat(dir)
			if err != nil {
				t.Fatalf("stat after redial: %v", err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no new connection 5s after the drop (err %v)", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if dials != 2 {
		t.Errorf("dials = %d, want 2", dials)
	}
}
