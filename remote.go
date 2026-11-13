package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const (
	keepAliveEvery   = 15 * time.Second
	keepAliveTimeout = 10 * time.Second
)

// remote holds one SSH/SFTP connection and dials again on the next request
// once it died: a Mac that slept or changed networks leaves the mount
// working instead of dead until a restart.
type remote struct {
	dial func(context.Context) (*ssh.Client, error)

	mu     sync.Mutex
	client *sftp.Client
}

func (r *remote) sftp(ctx context.Context) (*sftp.Client, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.client != nil {
		return r.client, nil
	}
	sc, err := r.dial(ctx)
	if err != nil {
		return nil, err
	}
	c, err := sftp.NewClient(sc)
	if err != nil {
		_ = sc.Close()
		return nil, fmt.Errorf("sftp: %w", err)
	}
	r.client = c
	go r.watch(sc, c)
	return c, nil
}

func (r *remote) watch(sc *ssh.Client, c *sftp.Client) {
	done := make(chan struct{})
	go keepAlive(sc, done)
	err := c.Wait()
	close(done)
	_ = sc.Close()

	r.mu.Lock()
	if r.client == c {
		r.client = nil
	}
	r.mu.Unlock()
	log.Printf("connection lost (%v), dialing again on the next request", err)
}

// keepAlive closes sc when the server stops answering: a link that died
// silently (sleep, network change) would otherwise hold every request.
// Same pattern as devmux/tunnel.go.
func keepAlive(sc *ssh.Client, done <-chan struct{}) {
	tick := time.NewTicker(keepAliveEvery)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return
		case <-tick.C:
		}
		err := bounded(sc, keepAliveTimeout, func() error {
			_, _, err := sc.SendRequest("keepalive@openssh.com", true, nil)
			return err
		})
		if err != nil {
			_ = sc.Close()
			return
		}
	}
}

func bounded(client io.Closer, d time.Duration, fn func() error) error {
	timer := time.AfterFunc(d, func() { _ = client.Close() })
	err := fn()
	if !timer.Stop() {
		return fmt.Errorf("no reply from the SSH server in %v", d)
	}
	return err
}
