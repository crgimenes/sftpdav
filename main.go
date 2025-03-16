package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/kevinburke/ssh_config"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/net/webdav"
)

type SFTPFileSystem struct {
	client    *sftp.Client
	remoteDir string
}

func (fsys *SFTPFileSystem) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	remotePath := path.Join(fsys.remoteDir, name)
	return fsys.client.MkdirAll(remotePath)
}

func (fsys *SFTPFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	remotePath := path.Join(fsys.remoteDir, name)
	file, err := fsys.client.OpenFile(remotePath, flag)
	if err != nil {
		return nil, err
	}
	return &sftpFile{
		File:       file,
		client:     fsys.client,
		remotePath: remotePath,
	}, nil
}

func (fsys *SFTPFileSystem) RemoveAll(ctx context.Context, name string) error {
	remotePath := path.Join(fsys.remoteDir, name)
	info, err := fsys.client.Stat(remotePath)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fsys.client.Remove(remotePath)
	}
	entries, err := fsys.client.ReadDir(remotePath)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		err = fsys.RemoveAll(ctx, path.Join(name, entry.Name()))
		if err != nil {
			return err
		}
	}
	return fsys.client.RemoveDirectory(remotePath)
}

func (fsys *SFTPFileSystem) Rename(ctx context.Context, oldName, newName string) error {
	oldPath := path.Join(fsys.remoteDir, oldName)
	newPath := path.Join(fsys.remoteDir, newName)
	return fsys.client.Rename(oldPath, newPath)
}

func (fsys *SFTPFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	remotePath := path.Join(fsys.remoteDir, name)
	return fsys.client.Stat(remotePath)
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
	end := f.readdirPos + count
	if end > len(f.readdirCache) {
		end = len(f.readdirCache)
	}
	entries := f.readdirCache[f.readdirPos:end]
	f.readdirPos = end
	return entries, nil
}

func (f *sftpFile) Stat() (os.FileInfo, error) {
	return f.File.Stat()
}

func loadPrivateKey(keyPath string) (ssh.Signer, error) {
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(key)
}

func main() {
	hostFlag := flag.String("host", "", "SSH host to connect to (as defined in ~/.ssh/config)")
	remoteDirFlag := flag.String("remoteDir", ".", "Remote directory to expose via WebDAV")
	localPortFlag := flag.String("port", "8811", "Local port for WebDAV server")
	flag.Parse()

	if *hostFlag == "" {
		log.Fatal("host flag is required")
	}

	sshConfigPath := os.ExpandEnv("$HOME/.ssh/config")
	file, err := os.Open(sshConfigPath)
	if err != nil {
		log.Fatalf("Failed to open SSH config file: %v", err)
	}
	defer file.Close()
	cfg, err := ssh_config.Decode(file)
	if err != nil {
		log.Fatalf("Failed to decode SSH config: %v", err)
	}

	user, _ := cfg.Get(*hostFlag, "User")
	if user == "" {
		user = os.Getenv("USER")
	}
	hostname, _ := cfg.Get(*hostFlag, "Hostname")
	if hostname == "" {
		hostname = *hostFlag
	}
	port, _ := cfg.Get(*hostFlag, "Port")
	if port == "" {
		port = "22"
	}
	identityFile, _ := cfg.Get(*hostFlag, "IdentityFile")
	if identityFile == "" {
		identityFile = os.ExpandEnv("$HOME/.ssh/id_rsa")
	} else {
		identityFile = os.ExpandEnv(identityFile)
	}

	log.Printf("identityFile: %s", identityFile)
	if strings.HasPrefix(identityFile, "~/") {
		identityFile = strings.Replace(identityFile, "~/", os.ExpandEnv("$HOME/"), 1)
	}
	log.Printf("identityFile: %s", identityFile)

	signer, err := loadPrivateKey(identityFile)
	if err != nil {
		log.Fatalf("Failed to load private key: %v", err)
	}

	hostKeyCallback, err := knownhosts.New(os.ExpandEnv("$HOME/.ssh/known_hosts"))
	if err != nil {
		log.Fatal("could not create hostkeycallback function: ", err)
	}

	sshClientConfig := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		//HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		HostKeyCallback: hostKeyCallback,
	}

	addr := fmt.Sprintf("%s:%s", hostname, port)
	sshClient, err := ssh.Dial("tcp", addr, sshClientConfig)
	if err != nil {
		log.Fatalf("Failed to dial SSH: %v", err)
	}
	defer sshClient.Close()

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		log.Fatalf("Failed to create SFTP client: %v", err)
	}
	defer sftpClient.Close()

	fsys := &SFTPFileSystem{
		client:    sftpClient,
		remoteDir: *remoteDirFlag,
	}

	handler := &webdav.Handler{
		Prefix:     "/",
		FileSystem: fsys,
		LockSystem: webdav.NewMemLS(),
	}

	log.Printf("Starting WebDAV server on :%s", *localPortFlag)
	log.Printf("Accessing SSH server %s@%s:%s", user, hostname, port)

	s := &http.Server{
		Handler:        handler,
		Addr:           ":" + *localPortFlag,
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   15 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	err = s.ListenAndServe()
	if err != nil {
		log.Fatal(err)
	}
}
