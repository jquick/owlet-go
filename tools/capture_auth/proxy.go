package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
)

type proxy struct {
	ca        *certAuthority
	capt      *capturer
	transport *http.Transport

	loggedMu sync.Mutex
	logged   map[string]bool
}

func newProxy(ca *certAuthority, capt *capturer) *proxy {
	return &proxy{
		ca:     ca,
		capt:   capt,
		logged: map[string]bool{},
		transport: &http.Transport{
			// We terminate TLS in the middle; trusting the upstream cert isn't
			// the point, capturing the plaintext is. This is a local debug tool.
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

// errOnce logs an error the first time a given key is seen, so a pinned or
// unreachable host doesn't flood the console.
func (p *proxy) errOnce(key, format string, args ...any) {
	p.loggedMu.Lock()
	defer p.loggedMu.Unlock()
	if p.logged[key] {
		return
	}
	p.logged[key] = true
	log.Printf(format, args...)
}

func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
		return
	}
	if strings.EqualFold(hostOnly(r.Host), "owlet.ca") { // CA download shortcut
		p.ca.serveCert(w)
		return
	}
	p.forward(w, r) // plain-HTTP proxying (absolute-URI request)
}

// handleConnect answers CONNECT, then hands the raw socket to the MITM loop.
func (p *proxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack unsupported", http.StatusInternalServerError)
		return
	}
	conn, _, err := hj.Hijack()
	if err != nil {
		return
	}
	if _, err := conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		conn.Close()
		return
	}
	go p.mitm(conn, hostOnly(r.Host))
}

// mitm terminates TLS toward the phone and relays each request to the origin,
// capturing along the way. WebSocket upgrades are tunneled through untouched.
func (p *proxy) mitm(raw net.Conn, host string) {
	defer raw.Close()

	tlsConn := tls.Server(raw, &tls.Config{
		GetCertificate: func(chi *tls.ClientHelloInfo) (*tls.Certificate, error) {
			name := chi.ServerName
			if name == "" {
				name = host
			}
			return p.ca.leafFor(name)
		},
	})
	if err := tlsConn.Handshake(); err != nil {
		p.errOnce("tls|"+host, "tls handshake with %s failed: %v (app may be pinning its cert)", host, err)
		return
	}

	br := bufio.NewReader(tlsConn)
	for {
		req, err := http.ReadRequest(br)
		if err != nil {
			return
		}
		req.URL.Scheme = "https"
		if req.URL.Host == "" {
			req.URL.Host = firstNonEmpty(req.Host, host)
		}
		if isUpgrade(req) {
			p.tunnel(tlsConn, br, req, host)
			return
		}
		if !p.relay(tlsConn, req) {
			return
		}
	}
}

// relay round-trips a MITM'd request and writes the response back over conn.
// It returns whether the connection should be kept alive for another request.
func (p *proxy) relay(conn net.Conn, req *http.Request) bool {
	resp, reqBody, respBody, err := p.exchange(req)
	if err != nil {
		p.errOnce("up|"+req.URL.Host+"|"+err.Error(), "upstream %s: %v", req.URL.Host, err)
		writeError(conn, err)
		return false
	}
	p.capt.record(req, reqBody, resp, respBody)
	if resp.Write(conn) != nil {
		return false
	}
	return !req.Close && !resp.Close
}

// forward handles plain (non-TLS) proxied requests.
func (p *proxy) forward(w http.ResponseWriter, r *http.Request) {
	if r.URL.Scheme == "" {
		r.URL.Scheme = "http"
	}
	resp, reqBody, respBody, err := p.exchange(r)
	if err != nil {
		p.errOnce("up|"+r.URL.Host+"|"+err.Error(), "upstream %s: %v", r.URL.Host, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	p.capt.record(r, reqBody, resp, respBody)
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

// exchange buffers the request body, forwards to the origin, and buffers the
// response body (so both can be captured and the response re-sent).
func (p *proxy) exchange(req *http.Request) (*http.Response, []byte, []byte, error) {
	var reqBody []byte
	if req.Body != nil {
		reqBody, _ = io.ReadAll(req.Body)
		req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(reqBody))
	}
	req.RequestURI = ""
	scrubHopHeaders(req.Header)

	resp, err := p.transport.RoundTrip(req)
	if err != nil {
		return nil, reqBody, nil, err
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(respBody))
	return resp, reqBody, respBody, nil
}

// tunnel opaquely relays an upgraded connection (e.g. WebSocket) to the origin.
func (p *proxy) tunnel(client net.Conn, br *bufio.Reader, req *http.Request, host string) {
	oc, err := tls.Dial("tcp", withPort(host), &tls.Config{InsecureSkipVerify: true, ServerName: host})
	if err != nil {
		return
	}
	defer oc.Close()
	if err := req.Write(oc); err != nil {
		return
	}
	go io.Copy(oc, br)
	io.Copy(client, oc)
}

var hopHeaders = []string{
	"Proxy-Connection", "Proxy-Authenticate", "Proxy-Authorization",
	"Connection", "Keep-Alive", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
}

func scrubHopHeaders(h http.Header) {
	for _, k := range hopHeaders {
		h.Del(k)
	}
}

func copyHeader(dst, src http.Header) {
	for k, vs := range src {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

func writeError(conn net.Conn, err error) {
	resp := &http.Response{
		StatusCode: http.StatusBadGateway,
		ProtoMajor: 1, ProtoMinor: 1,
		Header: http.Header{},
		Body:   io.NopCloser(strings.NewReader(err.Error())),
	}
	resp.Write(conn)
}

func isUpgrade(r *http.Request) bool {
	return r.Header.Get("Upgrade") != "" || strings.EqualFold(r.Header.Get("Connection"), "upgrade")
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

func withPort(host string) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	return net.JoinHostPort(host, "443")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
