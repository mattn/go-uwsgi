package main

import (
	"github.com/mattn/go-uwsgi"
	"path/filepath"
	"net"
	"net/http"
	"os"

)

func main() {
	s := "/tmp/uwsgi.sock"
	os.Remove(s)
	l, e := net.Listen("unix", s)
	os.Chmod(s, 0666)
	if e != nil {
		println(e.Error())
		os.Exit(1)
	}
    http.Serve(&uwsgi.Listener{l}, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		path := req.URL.Path
		file := filepath.Join(".", filepath.FromSlash(path))
		f, e := os.Stat(file)
		if e == nil && f.IsDir() && path[len(path)-1] != '/' {
			rw.Header().Set("Location", req.URL.Path + "/")
			rw.WriteHeader(http.StatusFound)
			return
		}
		http.ServeFile(rw, req, filepath.Join(".", filepath.FromSlash(req.URL.Path)))
	}))
}
