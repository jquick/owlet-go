// Command owlet-cam is the Owlet Cam bridge: it connects to the camera over
// Kalay P2P (via the stock ThroughTek SDK in internal/tutk), pulls the raw
// H.264 + AAC FIFOs, and serves a browser player that decodes them with
// WebCodecs. No ffmpeg, no muxing, no re-encoding.
package main

import (
	"fmt"
	"log"

	"owletcam"
)

func main() {
	log.SetFlags(log.Ltime)
	log.SetPrefix("owlet ")
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

// run loads config, starts the camera stream, and serves the player.
func run() error {
	cfg, err := owletcam.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	mc := owletcam.NewMulticast()
	go owletcam.Stream(cfg, mc)

	addr := fmt.Sprintf(":%d", cfg.HTTPPort)
	srv := owletcam.NewServer(mc)
	if !cfg.TLS {
		log.Printf("serving player at http://0.0.0.0%s/", addr)
		return srv.ListenAndServe(addr)
	}

	cert, err := owletcam.ServerCert(cfg)
	if err != nil {
		return fmt.Errorf("tls cert: %w", err)
	}
	log.Printf("serving player at https://0.0.0.0%s/ (self-signed cert; accept the browser warning)", addr)
	return srv.ListenAndServeTLS(addr, cert)
}
