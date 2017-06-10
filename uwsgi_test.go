package uwsgi

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"testing"
	"time"
)

func writeKV(fd io.Writer, k, v string) {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], uint16(len(k)))
	fd.Write(b[:])
	fd.Write([]byte(k))
	binary.LittleEndian.PutUint16(b[:], uint16(len(v)))
	fd.Write(b[:])
	fd.Write([]byte(v))
}

func TestBasic(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	addr, _ := l.Addr().(*net.TCPAddr)

	var lastReq *http.Request
	reqNum := 0
	handler := http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		reqNum++

		v := fmt.Sprintf("bar%d", reqNum)
		if req.FormValue("foo") == v {
			fmt.Fprintf(res, "req=%d", reqNum)
		}
		lastReq = req
	})

	server := &http.Server{Handler: handler}
	go server.Serve(&Listener{l})

	m := map[string]string{
		"HOST":              "localhost",
		"REQUEST_METHOD":    "POST",
		"REQUEST_URI":       "/foo",
		"CONTENT_LENGTH":    "8",
		"SERVER_PROTOCOL":   "HTTP/1.1",
		"HTTP_CONTENT_TYPE": "application/x-www-form-urlencoded",
		"HTTP_USER_AGENT":   "go",
	}

	var b [2]byte
	var head [4]byte
	for n := 1; n <= 3; n++ {
		fd, _ := net.Dial("tcp", addr.String())
		s := 0
		for k, v := range m {
			s += (len([]byte(k)) + len([]byte(v)) + 4)
		}
		binary.LittleEndian.PutUint16(b[:], uint16(s))
		head[1] = b[0]
		head[2] = b[1]
		fd.Write(head[:])
		for k, v := range m {
			writeKV(fd, k, v)
		}
		fmt.Fprintf(fd, "foo=bar%d", n)
		time.Sleep(1e9)

		res, _ := http.ReadResponse(bufio.NewReader(fd), lastReq)
		got := res.Request.Method
		expected := "POST"
		if string(got) != expected {
			t.Errorf("Unexpected response for request #1; got %q; expected %q",
				string(got), expected)
		}

		got = res.Request.URL.Path
		expected = "/foo"
		if string(got) != expected {
			t.Errorf("Unexpected response for request #1; got %q; expected %q",
				string(got), expected)
		}
		body, _ := ioutil.ReadAll(res.Body)
		res.Body.Close()
		got = string(body)
		expected = fmt.Sprintf("req=%d", n)
		if string(got) != expected {
			t.Errorf("Unexpected response for request #1; got %q; expected %q",
				string(got), expected)
		}
		fd.Close()
		fd = nil
	}

	l.Close()
}

func TestServer(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	addr, _ := l.Addr().(*net.TCPAddr)

	var lastReq *http.Request

	passenger := &Passenger{"tcp", "127.0.0.1"}
	handler := http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		passenger.ServeHTTP(res, req)
		panic("stop")
	})
	server := &http.Server{Handler: handler}
	go server.ListenAndServe()

	m := map[string]string{
		"REQUEST_METHOD":    "POST",
		"REQUEST_URI":       "/foo",
		"CONTENT_LENGTH":    "8",
		"SERVER_PROTOCOL":   "HTTP/1.1",
		"HTTP_CONTENT_TYPE": "application/x-www-form-urlencoded",
		"HTTP_USER_AGENT":   "go",
	}
	var b [2]byte
	var head [4]byte
	for n := 1; n <= 3; n++ {
		fd, _ := net.Dial("tcp", addr.String())
		s := 0
		for k, v := range m {
			s += (len([]byte(k)) + len([]byte(v)) + 4)
		}
		binary.LittleEndian.PutUint16(b[:], uint16(s))
		head[1] = b[0]
		head[2] = b[1]
		fd.Write(head[:])
		for k, v := range m {
			writeKV(fd, k, v)
		}
		fmt.Fprintf(fd, "foo=bar%d", n)
		time.Sleep(1e9)

		res, _ := http.ReadResponse(bufio.NewReader(fd), lastReq)
		got := res.Request.Method
		expected := "POST"
		if string(got) != expected {
			t.Errorf("Unexpected response for request #1; got %q; expected %q",
				string(got), expected)
		}

		got = res.Request.URL.Path
		expected = "/foo"
		if string(got) != expected {
			t.Errorf("Unexpected response for request #1; got %q; expected %q",
				string(got), expected)
		}
		body, _ := ioutil.ReadAll(res.Body)
		res.Body.Close()
		got = string(body)
		expected = fmt.Sprintf("req=%d", n)
		if string(got) != expected {
			t.Errorf("Unexpected response for request #1; got %q; expected %q",
				string(got), expected)
		}
		fd.Close()
		fd = nil
	}

	l.Close()
}
