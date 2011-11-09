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
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
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

func (c *Conn) SetTimeout(s int64) error {
	return c.writer.SetTimeout(s)
}

func (c *Conn) SetReadTimeout(s int64) error {
	return c.writer.SetReadTimeout(s)
}

func (c *Conn) SetWriteTimeout(s int64) error {
	return c.writer.SetWriteTimeout(s)
}

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

	cl, _ := strconv.Atoi64(c.env["CONTENT_LENGTH"])
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

	buf.Write([]byte("\r\n"))

	if cl > 0 {
		io.CopyN(buf, fd, cl)
	}

	return c, nil
}
