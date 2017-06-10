package main

import (
	"flag"
	"github.com/mattn/go-uwsgi"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var server = flag.String("s", "unix:///tmp/uwsgi.sock", "server address")

func main() {
	flag.Parse()

	var l net.Listener
	var e error
	var s string
	if strings.HasPrefix(*server, "unix://") {
		s = (*server)[7:]
		os.Remove(s)
		l, e = net.Listen("unix", s)
		os.Chmod(s, 0666)
	} else if strings.HasPrefix(*server, "tcp://") {
		s = (*server)[6:]
		l, e = net.Listen("tcp", s)
	} else {
		flag.PrintDefaults()
		os.Exit(1)
	}
	if e != nil {
		println(e.Error())
		os.Exit(1)
	}
	root, _ := filepath.Split(os.Args[0])
	root, _ = filepath.Abs(root)
	http.Serve(&uwsgi.Listener{l}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		script_name := r.Header.Get("SCRIPT_NAME")
		path := r.URL.Path
		if strings.HasPrefix(path, script_name) {
			path = path[len(script_name):]
		}
		file := filepath.Join(root, filepath.FromSlash(path))
		f, e := os.Stat(file)
		if e == nil && f.IsDir() && len(path) > 0 && path[len(path)-1] != '/' {
			w.Header().Set("Location", r.URL.Path+"/")
			w.WriteHeader(http.StatusFound)
			return
		}

		http.ServeFile(w, r, file)
	}))
}
