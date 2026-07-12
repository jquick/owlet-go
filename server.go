package owletcam

import (
	"bufio"
	"crypto/tls"
	"embed"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

//go:embed assets
var assets embed.FS

// assetFS is the player UI (index.html, app.js, style.css, worklet.js), baked
// into the binary at build time.
func assetFS() fs.FS {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	return sub
}

type Server struct {
	mc     *Multicast
	static http.Handler
}

func NewServer(mc *Multicast) *Server {
	return &Server{mc: mc, static: http.FileServerFS(assetFS())}
}

func (s *Server) server(addr string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.route)
	return &http.Server{Addr: addr, Handler: mux}
}

func (s *Server) ListenAndServe(addr string) error {
	return s.server(addr).ListenAndServe()
}

func (s *Server) ListenAndServeTLS(addr string, cert tls.Certificate) error {
	srv := s.server(addr)
	// Pin HTTP/1.1: h2 doesn't support the connection hijack our WebSocket needs.
	srv.TLSConfig = &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"http/1.1"},
	}
	return srv.ListenAndServeTLS("", "")
}

func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		s.serveWS(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.static.ServeHTTP(w, r)
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Sec-WebSocket-Key")
	hj, ok := w.(http.Hijacker)
	if key == "" || !ok {
		http.Error(w, "bad websocket request", http.StatusBadRequest)
		return
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()

	if _, err := brw.WriteString("HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + wsAccept(key) + "\r\n\r\n"); err != nil {
		return
	}
	if err := brw.Flush(); err != nil {
		return
	}

	v := newViewer()
	s.mc.add(v)
	log.Printf("viewer connected (%d total)", s.mc.Count())
	defer func() {
		s.mc.remove(v)
		log.Printf("viewer disconnected (%d total)", s.mc.Count())
	}()

	go drain(conn, brw.Reader) // detect client close, shut the conn

	for frame := range v.ch {
		conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
		if _, err := conn.Write(frame); err != nil {
			return
		}
	}
}

// drain discards anything the client sends; on read error it closes the conn so
// the writer loop's next Write fails and the handler returns.
func drain(conn net.Conn, r *bufio.Reader) {
	buf := make([]byte, 512)
	for {
		if _, err := r.Read(buf); err != nil {
			conn.Close()
			return
		}
	}
}
