// Package owletcam is an Owlet Cam bridge: it connects to the camera over Kalay
// P2P (via the stock ThroughTek SDK in internal/tutk), pulls the raw H.264 + AAC
// FIFOs, and serves a browser player that decodes them with WebCodecs. No
// ffmpeg, no muxing, no re-encoding.
package owletcam

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config is the fully resolved runtime configuration, read from the environment
// (a .env file is loaded first as a base; real env vars win).
type Config struct {
	UID            string
	AuthKey        string
	Account        string
	Password       string
	Channel        int
	SDKKey         string
	Region         int
	Quality        string
	Audio          bool
	HTTPPort       int
	ConnectTimeout int
	IdleTimeout    int // seconds to keep the camera warm after the last viewer
	LanSearchPort  int
	TLS            bool   // serve HTTPS (self-signed unless TLSCert/TLSKey given)
	TLSCert        string // optional PEM cert path
	TLSKey         string // optional PEM key path
}

// Load resolves the configuration from the environment, seeding it from a .env
// file first. The three per-camera secrets (TUTK_UID, AUTH_KEY, PASSWORD) and
// the SDK_KEY are required; see .env.sample.
func Load() (Config, error) {
	loadDotEnv()

	c := Config{
		UID:            os.Getenv("TUTK_UID"),
		AuthKey:        os.Getenv("AUTH_KEY"),
		Account:        env("AV_ACCOUNT", "admin"),
		Password:       os.Getenv("PASSWORD"),
		Channel:        atoi(os.Getenv("CHANNEL"), 0),
		SDKKey:         os.Getenv("SDK_KEY"),
		Region:         atoi(os.Getenv("TUTK_REGION"), 3),
		Quality:        env("QUALITY", "high"),
		Audio:          boolEnv("AUDIO", true),
		HTTPPort:       atoi(os.Getenv("HTTP_PORT"), 8091),
		ConnectTimeout: atoi(os.Getenv("CONNECT_TIMEOUT"), 20),
		IdleTimeout:    atoi(os.Getenv("IDLE_TIMEOUT"), 600),
		LanSearchPort:  atoi(os.Getenv("LAN_PORT"), 63616),
		TLS:            boolEnv("TLS", true),
		TLSCert:        os.Getenv("TLS_CERT"),
		TLSKey:         os.Getenv("TLS_KEY"),
	}

	var missing []string
	for _, r := range []struct {
		name, val string
	}{{"TUTK_UID", c.UID}, {"AUTH_KEY", c.AuthKey}, {"PASSWORD", c.Password}, {"SDK_KEY", c.SDKKey}} {
		if r.val == "" {
			missing = append(missing, r.name)
		}
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required config: %s (copy .env.sample to .env and fill it in)", strings.Join(missing, ", "))
	}
	return c, nil
}

// loadDotEnv seeds os.Environ from a .env file without overriding vars that are
// already set (so Docker's env_file / -e always win). ENV_FILE overrides the
// path; otherwise ./.env then ../.env are tried.
func loadDotEnv() {
	paths := []string{".env", "../.env"}
	if p := os.Getenv("ENV_FILE"); p != "" {
		paths = []string{p}
	}
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, val, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			val = strings.Trim(strings.TrimSpace(val), `"'`)
			if _, set := os.LookupEnv(key); !set {
				os.Setenv(key, val)
			}
		}
		f.Close()
		return
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func atoi(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

func boolEnv(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v != "0" && v != "false"
}
