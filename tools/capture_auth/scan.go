package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// capturer pulls the three per-camera credentials out of the app's traffic and
// stays quiet about everything else. Full flows are only written to disk when
// -save is set.
type capturer struct {
	dir     string
	save    bool
	all     bool
	verbose bool
	outFile string

	mu    sync.Mutex
	found map[string]string
	done  bool
	count atomic.Int64
}

func newCapturer(dir, outFile string, save, all, verbose bool) (*capturer, error) {
	if save {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	return &capturer{dir: dir, outFile: outFile, save: save, all: all, verbose: verbose, found: map[string]string{}}, nil
}

// Hosts we care about (case-insensitive substrings). Everything else is ignored
// unless -all is set.
var interestingHosts = []string{
	"owlet", "ayla", "aylanetworks", "throughtek", "tutk", "kalay", "iotcplatform",
}

// target maps a credential field to the (normalized) JSON keys that carry it.
type target struct {
	field   string
	aliases []string
}

var targets = []target{
	{"tutk_uid", []string{"tutkid", "tutkuid", "uid", "p2pid", "p2pdid"}},
	{"auth_key", []string{"authkey"}},
	{"password", []string{"password", "passwd", "pwd"}},
}

func (t target) matches(norm string) bool {
	for _, a := range t.aliases {
		if norm == a {
			return true
		}
	}
	return false
}

var (
	reUID      = regexp.MustCompile(`[A-Z0-9]{20}`)
	reNonAlnum = regexp.MustCompile(`[^a-z0-9]`)
	reSafeHost = regexp.MustCompile(`[^a-zA-Z0-9._-]`)
)

func (c *capturer) record(req *http.Request, reqBody []byte, resp *http.Response, respBody []byte) {
	host := hostOnly(req.Host)
	if host == "" {
		host = req.URL.Hostname()
	}
	if !c.all && !isInterestingHost(host) {
		return
	}
	if c.save {
		c.saveFlow(req, reqBody, resp, respBody, host)
	}
	c.extract(reqBody)
	c.extract(respBody)
}

// extract flattens a JSON body and records any target credential fields it finds.
func (c *capturer) extract(body []byte) {
	data := tryJSON(body)
	if data == nil {
		return
	}
	var leaves []kv
	walk(data, "", &leaves)

	c.mu.Lock()
	defer c.mu.Unlock()
	for _, l := range leaves {
		s, ok := l.value.(string)
		if !ok || s == "" {
			continue
		}
		norm := normLeaf(l.key)
		for _, t := range targets {
			if !t.matches(norm) {
				continue
			}
			val := s
			if t.field == "tutk_uid" { // must look like a real 20-char UID
				if m := reUID.FindString(s); m != "" {
					val = m
				} else {
					continue
				}
			}
			if c.found[t.field] == "" {
				c.found[t.field] = val
				log.Printf("found %-9s = %s", t.field, val)
			}
		}
	}
	c.announceIfComplete()
}

func (c *capturer) announceIfComplete() {
	if c.done {
		return
	}
	for _, t := range targets {
		if c.found[t.field] == "" {
			return
		}
	}
	c.done = true
	snippet, _ := json.MarshalIndent(c.found, "", "  ")
	highlight("captured all camera credentials:\n" + string(snippet))
	if c.outFile != "" {
		c.writeOut()
	}
}

// writeOut merges the captured fields into a .env file as KEY=VALUE lines,
// preserving anything else already in it (e.g. SDK_KEY) and any comments.
func (c *capturer) writeOut() {
	updates := make(map[string]string, len(c.found))
	for field, v := range c.found { // tutk_uid -> TUTK_UID, etc.
		updates[strings.ToUpper(field)] = v
	}
	if err := mergeEnv(c.outFile, updates); err != nil {
		log.Printf("write %s: %v", c.outFile, err)
		return
	}
	log.Printf("wrote %s", c.outFile)
}

// mergeEnv rewrites existing KEY= lines in place and appends any new keys,
// leaving comments and other lines untouched.
func mergeEnv(path string, updates map[string]string) error {
	var lines []string
	if b, err := os.ReadFile(path); err == nil {
		lines = strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	}
	seen := map[string]bool{}
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		key, _, ok := strings.Cut(ln, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if v, want := updates[key]; want {
			lines[i] = key + "=" + v
			seen[key] = true
		}
	}
	keys := make([]string, 0, len(updates))
	for k := range updates {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !seen[k] {
			lines = append(lines, k+"="+updates[k])
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

func (c *capturer) saveFlow(req *http.Request, reqBody []byte, resp *http.Response, respBody []byte, host string) {
	n := c.count.Add(1)
	rec := map[string]any{
		"captured_at":      time.Now().Format("2006-01-02 15:04:05"),
		"method":           req.Method,
		"url":              req.URL.String(),
		"request_headers":  headerMap(req.Header),
		"request_body":     bodyOrText(reqBody),
		"status_code":      resp.StatusCode,
		"response_headers": headerMap(resp.Header),
		"response_body":    bodyOrText(respBody),
	}
	name := fmt.Sprintf("%04d_%s.json", n, reSafeHost.ReplaceAllString(host, "_"))
	if b, err := json.MarshalIndent(rec, "", "  "); err == nil {
		os.WriteFile(filepath.Join(c.dir, name), b, 0o644)
	}
	if c.verbose {
		log.Printf("saved %s  (%s %s)", name, req.Method, req.URL)
	}
}

func isInterestingHost(host string) bool {
	h := strings.ToLower(host)
	for _, s := range interestingHosts {
		if strings.Contains(h, s) {
			return true
		}
	}
	return false
}

func normLeaf(dotted string) string {
	leaf := dotted
	if i := strings.LastIndex(leaf, "."); i >= 0 {
		leaf = leaf[i+1:]
	}
	if i := strings.Index(leaf, "["); i >= 0 {
		leaf = leaf[:i]
	}
	return reNonAlnum.ReplaceAllString(strings.ToLower(leaf), "")
}

type kv struct {
	key   string
	value any
}

func walk(obj any, path string, out *[]kv) {
	switch t := obj.(type) {
	case map[string]any:
		for k, v := range t {
			p := k
			if path != "" {
				p = path + "." + k
			}
			walk(v, p, out)
		}
	case []any:
		for i, v := range t {
			walk(v, fmt.Sprintf("%s[%d]", path, i), out)
		}
	default:
		*out = append(*out, kv{path, obj})
	}
}

func tryJSON(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return v
}

func bodyOrText(raw []byte) any {
	if v := tryJSON(raw); v != nil {
		return v
	}
	return safeText(raw)
}

func headerMap(h http.Header) map[string]any {
	m := make(map[string]any, len(h))
	for k, vs := range h {
		m[k] = strings.Join(vs, ", ")
	}
	return m
}

func safeText(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	if !utf8.Valid(raw) {
		return fmt.Sprintf("<%d bytes binary>", len(raw))
	}
	s := string(raw)
	if len(s) > 20000 {
		return s[:20000] + "...<truncated>"
	}
	return s
}

func highlight(msg string) {
	bar := strings.Repeat("=", 60)
	log.Printf("\n%s\n%s\n%s", bar, msg, bar)
}
