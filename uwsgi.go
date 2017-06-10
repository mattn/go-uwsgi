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
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Listener behave as net.Listener
type Listener struct {
	net.Listener
}

// Conn is connection for uWSGI
type Conn struct {
	net.Conn
	env     map[string][]string
	reader  io.Reader
	hdrdone bool
	ready   bool
	readych chan bool
	err     error
}

func (c *Conn) Read(b []byte) (n int, e error) {
	// Wait until headers have been processed
	if !c.ready && c.err == nil {
		<-c.readych
		c.ready = true
	}
	if c.err != nil {
		return 0, c.err
	}

	// After headers have been read by HTTP server, transfer
	// socket over to the underlying connection for direct read.
	if !c.hdrdone {
		n, e = c.reader.Read(b)
		if n == 0 || e != nil {
			c.hdrdone = true
		}
	}
	if c.hdrdone {
		n, e = c.Conn.Read(b)
		c.err = e
	}

	return n, e
}

// Writer behave as same as net.Listener
func (c *Conn) Write(b []byte) (int, error) {
	if c.err != nil {
		return 0, c.err
	}

	return c.Conn.Write(b)
}

// SetDeadline behave as same as net.Listener
func (c *Conn) SetDeadline(t time.Time) error {
	if c.err != nil {
		return c.err
	}

	return c.Conn.SetDeadline(t)
}

// SetReadDeadline behave as same as net.Listener
func (c *Conn) SetReadDeadline(t time.Time) error {
	if c.err != nil {
		return c.err
	}

	return c.Conn.SetReadDeadline(t)
}

// SetWriteDeadline behave as same as net.Listener
func (c *Conn) SetWriteDeadline(t time.Time) error {
	if c.err != nil {
		return c.err
	}

	return c.Conn.SetWriteDeadline(t)
}

var headerMappings = map[string]string{
	"HTTP_HOST":              "Host",
	"CONTENT_TYPE":           "Content-Type",
	"HTTP_ACCEPT":            "Accept",
	"HTTP_ACCEPT_ENCODING":   "Accept-Encoding",
	"HTTP_ACCEPT_LANGUAGE":   "Accept-Language",
	"HTTP_ACCEPT_CHARSET":    "Accept-Charset",
	"HTTP_CONTENT_TYPE":      "Content-Type",
	"HTTP_COOKIE":            "Cookie",
	"HTTP_IF_MATCH":          "If-Match",
	"HTTP_IF_MODIFIED_SINCE": "If-Modified-Since",
	"HTTP_IF_NONE_MATCH":     "If-None-Match",
	"HTTP_IF_RANGE":          "If-Range",
	"HTTP_RANGE":             "Range",
	"HTTP_REFERER":           "Referer",
	"HTTP_USER_AGENT":        "User-Agent",
	"HTTP_X_REQUESTED_WITH":  "Requested-With",
}

// Accept conduct as net.Listener. uWSGI protocol is working good for CGI.
// This function parse headers and pass to the Server.
func (l *Listener) Accept() (net.Conn, error) {
	fd, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}

	buf := new(bytes.Buffer)
	c := &Conn{fd, make(map[string][]string), buf, false, false, make(chan bool, 1), nil}

	go func() {
		/*
		 * uwsgi header:
		 * struct {
		 *    uint8  modifier1;
		 *    uint16 datasize;
		 *    uint8  modifier2;
		 * }
		 *  -- for HTTP, mod1 and mod2 = 0
		 */
		var head [4]byte
		fd.Read(head[:])
		b := []byte{head[1], head[2]}
		envsize := binary.LittleEndian.Uint16(b)

		envbuf := make([]byte, envsize)
		if _, err := io.ReadFull(fd, envbuf); err != nil {
			fd.Close()
			c.err = err
			return
		}

		/*
		 * uwsgi vars are linear lists of the form:
		 * struct {
		 *   uint16 key_size;
		 *   uint8  key[key_size];
		 *   uint16 val_size;
		 *   uint8  val[val_size];
		 * }
		 */
		i := uint16(0)
		var reqMethod string
		var reqURI string
		var reqProtocol string
		for {
			// Ensure no corrupted payload; shouldn't happen but it has...
			if i+1 >= uint16(len(envbuf)) {
				break
			}
			b := []byte{envbuf[i], envbuf[i+1]}
			kl := binary.LittleEndian.Uint16(b)
			i += 2

			if i+kl > uint16(len(envbuf)) {
				fd.Close()
				c.err = errors.New("Invalid uwsgi request; uwsgi vars index out of range")
				return
			}

			k := string(envbuf[i : i+kl])
			i += kl

			if i+1 >= uint16(len(envbuf)) {
				fd.Close()
				c.err = errors.New("Invalid uwsgi request; uwsgi vars index out of range")
				return
			}

			b = []byte{envbuf[i], envbuf[i+1]}
			vl := binary.LittleEndian.Uint16(b)
			i += 2

			if i+vl > uint16(len(envbuf)) {
				fd.Close()
				c.err = errors.New("Invalid uwsgi request; uwsgi vars index out of range")
				return
			}

			v := string(envbuf[i : i+vl])
			i += vl

			if k == "REQUEST_METHOD" {
				reqMethod = v
			} else if k == "REQUEST_URI" {
				reqURI = v
			} else if k == "SERVER_PROTOCOL" {
				reqProtocol = v
			}

			val, ok := c.env[k]
			if !ok {
				val = make([]string, 0, 2)
			}
			val = append(val, v)
			c.env[k] = val

			if i >= envsize {
				break
			}
		}

		if reqProtocol == "" {
			// Invalid protocol
			fd.Close()
			c.err = errors.New("Invalid uwsgi request; no protocol specified")
			return
		}

		fmt.Fprintf(buf, "%s %s %s\r\n", reqMethod, reqURI, reqProtocol)

		var cl int64
		for i := range c.env {
			switch i {
			case "CONTENT_LENGTH":
				cl, _ = strconv.ParseInt(c.env[i][0], 10, 64)
				if cl > 0 {
					fmt.Fprintf(buf, "Content-Length: %d\r\n", cl)
				}
			default:
				hname, ok := headerMappings[i]
				if !ok {
					hname = i
				}
				for v := range c.env[i] {
					fmt.Fprintf(buf, "%s: %s\r\n", hname, c.env[i][v])
				}
			}
		}

		buf.Write([]byte("\r\n"))

		// Signal to indicate header processing is complete and remaining
		// payload can be read from the socket itself.
		c.readych <- true
	}()

	return c, nil
}

// Passenger works as uWSGI transport
type Passenger struct {
	Net  string
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
