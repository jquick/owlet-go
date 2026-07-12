// Command capture_auth is a tiny HTTPS-intercepting proxy that records the
// Owlet app's own traffic so you can pull the per-camera credentials
// (TUTK P2P UID, AuthKey, password) out of it for a local-only bridge.
//
// It is the Go replacement for the old mitmproxy addon: point your phone's
// Wi-Fi proxy at this machine, trust the generated CA once, and open the Owlet
// app. Nothing here attacks the camera -- it only observes traffic your phone
// is already sending.
//
// By default it's quiet: it only prints the credentials as it finds them (and
// any real errors). Point it at a .env file with -out to have them written for
// you, or use -save to also dump every full flow to disk for inspection.
//
// Usage (from the tools/ directory):
//
//	go run ./capture_auth
//	go run ./capture_auth -out ../.env
//	go run ./capture_auth -save -all      # forensic: dump everything
package main

import (
	"flag"
	"log"
	"net"
	"net/http"
)

func main() {
	addr := flag.String("addr", ":8080", "proxy listen address")
	out := flag.String("out", "", "merge captured creds into this .env file (e.g. ../.env)")
	save := flag.Bool("save", false, "also save every full flow as JSON under -dir")
	dir := flag.String("dir", "captures", "directory for -save flows")
	all := flag.Bool("all", false, "look at every host, not just Owlet/TUTK ones")
	verbose := flag.Bool("v", false, "log each captured flow")
	caCert := flag.String("ca-cert", "capture-ca.pem", "CA certificate path (created if missing)")
	caKey := flag.String("ca-key", "capture-ca-key.pem", "CA private key path (created if missing)")
	flag.Parse()

	log.SetFlags(log.Ltime)

	ca, err := newCertAuthority(*caCert, *caKey)
	if err != nil {
		log.Fatalf("ca: %v", err)
	}
	capt, err := newCapturer(*dir, *out, *save, *all, *verbose)
	if err != nil {
		log.Fatalf("captures: %v", err)
	}

	printInstructions(*addr, *caCert)

	srv := &http.Server{Addr: *addr, Handler: newProxy(ca, capt)}
	log.Fatal(srv.ListenAndServe())
}

func printInstructions(addr, caPath string) {
	port := addr
	if _, p, err := net.SplitHostPort(addr); err == nil && p != "" {
		port = p
	}
	log.Printf("owlet capture proxy listening on %s  (CA: %s)", addr, caPath)
	log.Print("phone setup:")
	log.Printf("  1. Wi-Fi proxy -> host %s  port %s  (same network as this machine)", localIP(), port)
	log.Print("  2. open http://owlet.ca in the phone browser to download the CA profile")
	log.Print("  3. iOS: install it under Settings > General > VPN & Device Management")
	log.Print("     then ENABLE it under Settings > General > About > Certificate Trust Settings")
	log.Print("  4. open the Owlet app, log in, open the live camera view")
	log.Print("waiting for credentials ...")
}

func localIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:53")
	if err != nil {
		return "<this-machine-ip>"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}
