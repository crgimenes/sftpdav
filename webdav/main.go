package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"golang.org/x/net/webdav"
)

const usageText = `usage: webdav [-dir dir] [-port port]

Serves a local directory as WebDAV on 127.0.0.1, to be reached through an
SSH tunnel: ssh -N -L 8811:localhost:8811 user@remote

flags:
`

func run(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("webdav", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.String("port", "8811", "local `port` for the WebDAV server")
	dir := fs.String("dir", "./", "`directory` to share")
	usage := func(w io.Writer) {
		_, _ = fmt.Fprint(w, usageText)
		fs.SetOutput(w)
		fs.PrintDefaults()
		fs.SetOutput(io.Discard)
	}

	err := fs.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		usage(stdout)
		return err
	}
	if err != nil {
		usage(os.Stderr)
		return err
	}

	handler := &webdav.Handler{
		Prefix:     "/",
		FileSystem: webdav.Dir(*dir),
		LockSystem: webdav.NewMemLS(),
	}

	// Loopback only: the share has no authentication of its own.
	s := &http.Server{
		Handler: handler,
		Addr:    net.JoinHostPort("127.0.0.1", *port),
		// no Read/WriteTimeout: they would cut a large transfer midway
		ReadHeaderTimeout: 15 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
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
