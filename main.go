package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/webdav"
)

const usageText = `usage: sftpdav -host alias [-remoteDir dir] [-port port]

Serves a remote directory, reached over SSH/SFTP, as WebDAV on 127.0.0.1.
The host is resolved through ~/.ssh/config; authentication uses ssh-agent,
or the host's IdentityFile; the host key must be in ~/.ssh/known_hosts.

flags:
`

const usageExample = `
example:
  sftpdav -host myremote -remoteDir /srv/data
  mount_webdav http://127.0.0.1:8811 /Volumes/data
`

type SFTPFileSystem struct {
	remote    *remote
	remoteDir string
}

// resolve keeps name inside remoteDir: the webdav handler passes the request
// path through raw, so "/../x" would otherwise escape it.
func (fsys *SFTPFileSystem) resolve(name string) string {
	return path.Join(fsys.remoteDir, path.Clean("/"+name))
}

func (fsys *SFTPFileSystem) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	c, err := fsys.remote.sftp(ctx)
	if err != nil {
		return err
	}
	return c.Mkdir(fsys.resolve(name))
}

func (fsys *SFTPFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	c, err := fsys.remote.sftp(ctx)
	if err != nil {
		return nil, err
	}
	remotePath := fsys.resolve(name)
	file, err := c.OpenFile(remotePath, flag)
	if err != nil {
		return nil, err
	}
	return &sftpFile{
		File:       file,
		client:     c,
		remotePath: remotePath,
	}, nil
}

func (fsys *SFTPFileSystem) RemoveAll(ctx context.Context, name string) error {
	if path.Clean("/"+name) == "/" {
		// same rule as webdav.Dir: a DELETE on the root would wipe remoteDir
		return os.ErrInvalid
	}
	c, err := fsys.remote.sftp(ctx)
	if err != nil {
		return err
	}
	// the client's RemoveAll uses Lstat: a symlink to a directory is unlinked,
	// never followed out of remoteDir
	return c.RemoveAll(fsys.resolve(name))
}

func (fsys *SFTPFileSystem) Rename(ctx context.Context, oldName, newName string) error {
	c, err := fsys.remote.sftp(ctx)
	if err != nil {
		return err
	}
	return c.Rename(fsys.resolve(oldName), fsys.resolve(newName))
}

func (fsys *SFTPFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	c, err := fsys.remote.sftp(ctx)
	if err != nil {
		return nil, err
	}
	return c.Stat(fsys.resolve(name))
}

type sftpFile struct {
	*sftp.File
	client     *sftp.Client
	remotePath string

	readdirCache []os.FileInfo
	readdirPos   int
}

func (f *sftpFile) Readdir(count int) ([]os.FileInfo, error) {
	if f.readdirCache == nil {
		entries, err := f.client.ReadDir(f.remotePath)
		if err != nil {
			return nil, err
		}
		f.readdirCache = entries
	}
	if count <= 0 {
		entries := f.readdirCache[f.readdirPos:]
		f.readdirPos = len(f.readdirCache)
		return entries, nil
	}
	if f.readdirPos >= len(f.readdirCache) {
		return nil, nil
	}
	end := min(f.readdirPos+count, len(f.readdirCache))
	entries := f.readdirCache[f.readdirPos:end]
	f.readdirPos = end
	return entries, nil
}

func run(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("sftpdav", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	host := fs.String("host", "", "SSH host `alias` from ~/.ssh/config, or a hostname")
	remoteDir := fs.String("remoteDir", ".", "remote `directory` to expose")
	port := fs.String("port", "8811", "local `port` for the WebDAV server")
	usage := func(w io.Writer) {
		_, _ = fmt.Fprint(w, usageText)
		fs.SetOutput(w)
		fs.PrintDefaults()
		fs.SetOutput(io.Discard)
		_, _ = fmt.Fprint(w, usageExample)
	}

	err := fs.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		usage(stdout)
		return err
	}
	if err == nil && *host == "" {
		err = errors.New("-host is required")
	}
	if err == nil && fs.NArg() > 0 {
		err = fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if err != nil {
		usage(os.Stderr)
		return err
	}

	r := &remote{dial: func(ctx context.Context) (*ssh.Client, error) {
		sc, err := dialSSH(ctx, *host)
		if err != nil {
			return nil, fmt.Errorf("ssh %s: %w", *host, err)
		}
		return sc, nil
	}}
	// dial now, so a wrong host or key fails here and not on the first mount
	_, err = r.sftp(context.Background())
	if err != nil {
		return err
	}

	handler := &webdav.Handler{
		Prefix: "/",
		FileSystem: &SFTPFileSystem{
			remote:    r,
			remoteDir: *remoteDir,
		},
		LockSystem: webdav.NewMemLS(),
	}

	// Loopback only: the share has no authentication of its own.
	addr := net.JoinHostPort("127.0.0.1", *port)
	s := &http.Server{
		Handler: handler,
		Addr:    addr,
		// no Read/WriteTimeout: they would cut a large transfer midway
		ReadHeaderTimeout: 15 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	log.Printf("serving %s:%s as WebDAV on http://%s", *host, *remoteDir, addr)
	return s.ListenAndServe()
}

func main() {
	err := run(os.Args[1:], os.Stdout)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		log.Fatal(err)
	}
}
