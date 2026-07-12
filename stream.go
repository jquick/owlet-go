package owletcam

import (
	"log"
	"runtime"
	"time"

	"owletcam/internal/tutk"
)

// Synthetic per-stream clocks (microseconds). Playback is render-on-decode, so
// these only need to be monotonic for the browser decoders to order frames.
const (
	videoStepUS = 1_000_000 / 30          // well above the real fps
	audioStepUS = 1_000_000 * 1024 / 8000 // one AAC-LC frame @ 8 kHz = 128 ms
)

// Stream keeps a camera session alive only while someone is watching: it waits
// for the first viewer, connects, broadcasts A/V (reconnecting with backoff on
// error), and drops the camera again once the last viewer leaves. TUTK
// serializes the A/V channel, so a single goroutine (pinned to one OS thread)
// interleaves video + audio.
func Stream(cfg Config, mc *Multicast) {
	runtime.LockOSThread()

	backoff := 2 * time.Second
	for {
		mc.WaitForViewer() // idle (no camera connection) while nobody's watching

		if err := session(cfg, mc); err != nil {
			log.Printf("session error: %v", err)
			log.Printf("reconnecting in %s...", backoff)
			time.Sleep(backoff)
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		} else {
			backoff = 2 * time.Second
		}
	}
}

func session(cfg Config, mc *Multicast) error {
	log.Printf("connecting uid=%s account=%s channel=%d quality=%s audio=%v",
		cfg.UID, cfg.Account, cfg.Channel, cfg.Quality, cfg.Audio)

	s, err := tutk.Open(tutk.Params{
		UID:            cfg.UID,
		AuthKey:        cfg.AuthKey,
		Account:        cfg.Account,
		Password:       cfg.Password,
		SDKKey:         cfg.SDKKey,
		Channel:        cfg.Channel,
		Region:         cfg.Region,
		ConnectTimeout: cfg.ConnectTimeout,
		LanSearchPort:  cfg.LanSearchPort,
	})
	if err != nil {
		return err
	}
	defer s.Close()
	log.Print("connected; A/V channel authenticated")

	s.StartStream(tutk.QualityFromName(cfg.Quality), cfg.Channel, cfg.Audio)

	var vts, ats uint64
	firstV, firstA := false, false
	frames := 0
	t0 := time.Now()

	// Keep the camera warm for a grace period after the last viewer leaves, so a
	// quick rejoin skips the reconnect delay.
	grace := time.Duration(cfg.IdleTimeout) * time.Second
	var idleSince time.Time

	for {
		switch {
		case mc.Count() > 0:
			idleSince = time.Time{} // someone's watching
		case idleSince.IsZero():
			idleSince = time.Now()
			log.Printf("no viewers; keeping camera warm for %s", grace)
		case time.Since(idleSince) >= grace:
			log.Print("no viewers past grace period; disconnecting camera")
			return nil
		}

		got := false

		if rc, data, info := s.RecvVideo(); rc < 0 && !tutk.Recoverable(rc) {
			if rc == tutk.ErrSessionCloseByRemote {
				time.Sleep(time.Second)
			}
			return nil // end session -> reconnect
		} else if len(data) > 0 {
			got = true
			key := isKeyframe(data)
			if !firstV {
				log.Printf("first video frame: codec=%d %dB key=%v", info.Codec, len(data), key)
				firstV = true
			}
			mc.BroadcastVideo(vts, data, key)
			vts += videoStepUS
			frames++
			if d := time.Since(t0); d >= 10*time.Second {
				log.Printf("video %.1f fps, viewers=%d", float64(frames)/d.Seconds(), mc.Count())
				frames, t0 = 0, time.Now()
			}
		}

		if cfg.Audio {
			if _, data, _ := s.RecvAudio(); len(data) > 0 {
				got = true
				if !firstA {
					log.Printf("first audio frame: %dB", len(data))
					firstA = true
				}
				mc.BroadcastAudio(ats, data)
				ats += audioStepUS
			}
		}

		if !got {
			time.Sleep(3 * time.Millisecond)
		}
	}
}

// isKeyframe reports whether an Annex-B access unit carries an SPS(7) or IDR(5)
// NAL, i.e. a clean point for a decoder to start. Scanning for the 3-byte start
// code also covers 4-byte ones (00 00 00 01 contains 00 00 01).
func isKeyframe(au []byte) bool {
	for i := 0; i+3 < len(au); i++ {
		if au[i] == 0 && au[i+1] == 0 && au[i+2] == 1 {
			if t := au[i+3] & 0x1f; t == 5 || t == 7 {
				return true
			}
		}
	}
	return false
}
