package proxy

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// ResponseEvent is emitted for every intercepted GET response with text content.
type ResponseEvent struct {
	Host        string
	Path        string
	Body        []byte
	ContentType string
}

// Interceptor is an HTTP forward proxy with HTTPS MITM support.
type Interceptor struct {
	CA         *CA
	OnResponse func(ResponseEvent)
}

func (p *Interceptor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.handleCONNECT(w, r)
		return
	}
	p.handleHTTP(w, r)
}

// handleHTTP forwards plain HTTP proxy requests.
func (p *Interceptor) handleHTTP(w http.ResponseWriter, r *http.Request) {
	r.RequestURI = ""
	r.Header.Del("Proxy-Connection")

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Do(r)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}

	if r.Method == http.MethodGet {
		p.emit(r.Host, r.URL.Path, r.URL.RawQuery, body, resp.Header.Get("Content-Type"))
	}

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

// handleCONNECT intercepts HTTPS tunnels via MITM.
func (p *Interceptor) handleCONNECT(w http.ResponseWriter, r *http.Request) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", 500)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer clientConn.Close()

	// Acknowledge the tunnel.
	clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	host := r.Host
	if !strings.Contains(host, ":") {
		host += ":443"
	}
	hostname := strings.Split(host, ":")[0]

	// Generate (or reuse cached) cert for this host.
	cert, err := p.CA.CertForHost(hostname)
	if err != nil {
		return
	}

	// Wrap the client connection in TLS using our MITM cert.
	tlsClientConn := tls.Server(clientConn, &tls.Config{
		Certificates: []tls.Certificate{*cert},
	})
	if err := tlsClientConn.Handshake(); err != nil {
		return
	}
	defer tlsClientConn.Close()

	// Transport that connects to the real upstream server.
	transport := &http.Transport{
		TLSClientConfig:     &tls.Config{ServerName: hostname},
		TLSHandshakeTimeout: 10 * time.Second,
		DisableKeepAlives:   false,
	}
	defer transport.CloseIdleConnections()

	// Process HTTP/1.x requests from the intercepted TLS connection.
	reader := bufio.NewReader(tlsClientConn)
	for {
		req, err := http.ReadRequest(reader)
		if err != nil {
			break
		}

		req.URL.Scheme = "https"
		req.URL.Host = host
		req.RequestURI = ""

		resp, err := transport.RoundTrip(req)
		if err != nil {
			break
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			break
		}

		if req.Method == http.MethodGet {
			p.emit(hostname, req.URL.Path, req.URL.RawQuery, body, resp.Header.Get("Content-Type"))
		}

		resp.Body = io.NopCloser(bytes.NewReader(body))
		if err := resp.Write(tlsClientConn); err != nil {
			break
		}
	}
}

func (p *Interceptor) emit(host, path, query string, body []byte, contentType string) {
	if p.OnResponse == nil || len(body) == 0 || !isTextContent(contentType) {
		return
	}
	fullPath := path
	if query != "" {
		fullPath = path + "?" + query
	}
	p.OnResponse(ResponseEvent{
		Host:        host,
		Path:        fullPath,
		Body:        body,
		ContentType: contentType,
	})
}

func copyHeaders(dst, src http.Header) {
	for k, vals := range src {
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}

func isTextContent(ct string) bool {
	ct = strings.ToLower(ct)
	return strings.Contains(ct, "json") ||
		strings.Contains(ct, "text/") ||
		strings.Contains(ct, "xml") ||
		strings.Contains(ct, "yaml")
}

// singleConnListener wraps a single net.Conn as a net.Listener.
// Used to serve one connection through http.Server.
type singleConnListener struct {
	ch   chan net.Conn
	addr net.Addr
}

func newSingleConnListener(conn net.Conn) *singleConnListener {
	l := &singleConnListener{
		ch:   make(chan net.Conn, 1),
		addr: conn.LocalAddr(),
	}
	l.ch <- conn
	return l
}

func (l *singleConnListener) Accept() (net.Conn, error) {
	conn, ok := <-l.ch
	if !ok {
		return nil, fmt.Errorf("listener closed")
	}
	return conn, nil
}

func (l *singleConnListener) Close() error {
	select {
	case <-l.ch:
	default:
	}
	return nil
}

func (l *singleConnListener) Addr() net.Addr { return l.addr }
