/*
This file implements the uWSGI protocol.
This implements run as net.Listener:


		l, err = net.Listen("unix", "/path/to/socket")
		http.Serve(&UwsgiListener{l}, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Length", 11)
			w.Write([]byte("hello world"))
		})
*/

package uwsgi

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"regexp"
	"time"
)

type Listener struct {
	Listener net.Listener
}

type Conn struct {
	env    map[string]string
	reader io.Reader
	writer net.Conn
}

func (c *Conn) Read(b []byte) (int, error) {
	n, e := c.reader.Read(b)
	return n, e
}

func (c *Conn) Write(b []byte) (int, error) {
	return c.writer.Write(b)
}

func (c *Conn) Close() error {
	return c.writer.Close()
}

func (c *Conn) LocalAddr() net.Addr {
	return c.writer.LocalAddr()
}

func (c *Conn) RemoteAddr() net.Addr {
	return c.writer.RemoteAddr()
}

func (c *Conn) SetDeadline(t time.Time) error {
	return c.writer.SetDeadline(t)
}

func (c *Conn) SetReadDeadline(t time.Time) error {
	return c.writer.SetReadDeadline(t)
}

func (c *Conn) SetWriteDeadline(t time.Time) error {
	return c.writer.SetWriteDeadline(t)
}

/*
func (c *Conn) SetTimeout(s int64) error {
	return c.writer.SetTimeout(s)
}
*/

/*
func (c *Conn) SetReadTimeout(s int64) error {
	return c.writer.SetReadTimeout(s)
}
*/

/*
func (c *Conn) SetWriteTimeout(s int64) error {
	return c.writer.SetWriteTimeout(s)
}
*/

func (l *Listener) Addr() net.Addr {
	return l.Listener.Addr()
}

func (l *Listener) Close() error {
	return l.Listener.Close()
}

// Accept conduct as net.Listener. uWSGI protocol is working good for CGI.
// This function parse headers and pass to the Server.
func (l *Listener) Accept() (net.Conn, error) {
	fd, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}

	buf := new(bytes.Buffer)
	c := &Conn{map[string]string{}, buf, fd}

	var head [4]byte
	fd.Read(head[:])
	//mod1 := head[0:0]
	b := []byte{head[1], head[2]}
	envsize := binary.LittleEndian.Uint16(b)
	//mod2 := head[3:3]

	envbuf := make([]byte, envsize)
	if _, err := io.ReadFull(fd, envbuf); err != nil {
		return nil, err
	}

	i := uint16(0)
	for {
		b := []byte{envbuf[i], envbuf[i+1]}
		kl := binary.LittleEndian.Uint16(b)
		i += 2
		k := string(envbuf[i : i+kl])
		i += kl
		b = []byte{envbuf[i], envbuf[i+1]}
		vl := binary.LittleEndian.Uint16(b)
		i += 2
		v := string(envbuf[i : i+vl])
		i += vl

		c.env[k] = v
		if i >= envsize {
			break
		}
	}

	//fmt.Fprintf(buf, "%s %s %s\r\n", c.env["REQUEST_METHOD"], c.env["REQUEST_URI"], c.env["SERVER_PROTOCOL"])
	fmt.Fprintf(buf, "%s %s %s\r\n", c.env["REQUEST_METHOD"], c.env["REQUEST_URI"], "HTTP/1.0")

	cl, _ := strconv.ParseInt(c.env["CONTENT_LENGTH"], 10, 64)
	if cl > 0 {
		fmt.Fprintf(buf, "Content-Length: %d\r\n", cl)
	}

	if v := c.env["HTTP_CONTENT_TYPE"]; len(v) > 0 {
		fmt.Fprintf(buf, "Content-Type: %s\r\n", v)
	}

	if v := c.env["HTTP_HOST"]; len(v) > 0 {
		fmt.Fprintf(buf, "Host: %s\r\n", v)
	}

	if v := c.env["HTTP_CONNECTION"]; len(v) > 0 {
		fmt.Fprintf(buf, "Connection: %s\r\n", v)
	}

	if v := c.env["HTTP_ACCEPT"]; len(v) > 0 {
		fmt.Fprintf(buf, "Accept: %s\r\n", v)
	}

	if v := c.env["HTTP_USER_AGENT"]; len(v) > 0 {
		fmt.Fprintf(buf, "User-Agent: %s\r\n", v)
	}

	if v := c.env["HTTP_ACCEPT_ENCODING"]; len(v) > 0 {
		fmt.Fprintf(buf, "Accept-Encoding: %s\r\n", v)
	}

	if v := c.env["HTTP_ACCEPT_LANGUAGE"]; len(v) > 0 {
		fmt.Fprintf(buf, "Accept-Language: %s\r\n", v)
	}

	if v := c.env["HTTP_ACCEPT_CHARSET"]; len(v) > 0 {
		fmt.Fprintf(buf, "Accept-Charset: %s\r\n", v)
	}

	if v, e := c.env["SCRIPT_NAME"]; e {
		fmt.Fprintf(buf, "SCRIPT_NAME: %s\r\n", v)
	}

	if v, e := c.env["PATH_INFO"]; e {
		fmt.Fprintf(buf, "PATH_INFO: %s\r\n", v)
	}

	buf.Write([]byte("\r\n"))

	if cl > 0 {
		io.CopyN(buf, fd, cl)
	}

	return c, nil
}

type Passenger struct {
	Net string
	Addr string
}

var trailingPort = regexp.MustCompile(`:([0-9]+)$`)
func (p Passenger) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	conn, err := net.Dial(p.Net, p.Addr)
	if err != nil {
		panic(err.Error())
	}
	defer conn.Close()

	port := "80"
	if matches := trailingPort.FindStringSubmatch(req.Host); len(matches) != 0 {
		port = matches[1]
	}

	header := make(map[string][]string)
	header["REQUEST_METHOD"] = []string{req.Method}
	header["REQUEST_URI"] = []string{req.RequestURI}
	header["CONTENT_LENGTH"] = []string{strconv.Itoa(int(req.ContentLength))}
	header["SERVER_PROTOCOL"] = []string{req.Proto}
	header["SERVER_NAME"] = []string{req.Host}
	header["SERVER_ADDR"] = []string{req.RemoteAddr}
	header["SERVER_PORT"] = []string{port}
	header["REMOTE_HOST"] = []string{req.RemoteAddr}
	header["REMOTE_ADDR"] = []string{req.RemoteAddr}
	header["SCRIPT_NAME"] = []string{req.URL.Path}
	header["PATH_INFO"] = []string{req.URL.Path}
	header["QUERY_STRING"] = []string{req.URL.RawQuery}
	if ctype := req.Header.Get("Content-Type"); ctype != "" {
		header["CONTENT_TYPE"] = []string{ctype}
	}
	for k, v := range req.Header {
		if _, ok := header[k]; ok == false {
			k = "HTTP_" + strings.ToUpper(strings.Replace(k, "-", "_", -1))
			header[k] = v
		}
	}

	var size uint16
	for k, v := range header {
		for _, vv := range v {
			size += uint16(len(([]byte)(k))) + 2
			size += uint16(len(([]byte)(vv))) + 2
		}
	}

	hsize := make([]byte, 4)
	binary.LittleEndian.PutUint16(hsize[1:3], size)
	conn.Write(hsize)

	for k, v := range header {
		for _, vv := range v {
			binary.Write(conn, binary.LittleEndian, uint16(len(([]byte)(k))))
			conn.Write([]byte(k))
			binary.Write(conn, binary.LittleEndian, uint16(len(([]byte)(vv))))
			conn.Write([]byte(vv))
		}
	}

	io.Copy(conn, req.Body)

	res, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		panic(err.Error())
	}
	for k, v := range res.Header {
		w.Header().Del(k)
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}
	io.Copy(w, res.Body)
}
