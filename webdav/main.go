package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"golang.org/x/net/webdav"
)

func main() {
	localPortFlag := flag.String("port", "8811", "Local port for WebDAV server")
	dirFlag := flag.String("dir", "./", "Directory to share")

	flag.Parse()

	handler := &webdav.Handler{
		Prefix:     "/",
		FileSystem: webdav.Dir(*dirFlag),
		LockSystem: webdav.NewMemLS(),
	}

	s := &http.Server{
		Handler:        handler,
		Addr:           ":" + *localPortFlag,
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   15 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	err := s.ListenAndServe()
	if err != nil {
		log.Fatal(err)
	}
}
