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
	root, _ := filepath.Split(os.Args[0])
	root, _ = filepath.Abs(root)
    http.Serve(&uwsgi.Listener{l}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		file := filepath.Join(root, filepath.FromSlash(path))
		f, e := os.Stat(file)
		if e == nil && f.IsDir() && path[len(path)-1] != '/' {
			w.Header().Set("Location", r.URL.Path + "/")
			w.WriteHeader(http.StatusFound)
			return
		}
		http.ServeFile(w, r, file)
	}))
}
