package owletcam

import "sync"

// viewer is one connected browser. Frames are pre-encoded WebSocket frames.
type viewer struct {
	ch         chan []byte
	waitingKey bool // hold audio+video until a clean keyframe start
}

func newViewer() *viewer {
	return &viewer{ch: make(chan []byte, 300), waitingKey: true}
}

// offer enqueues a frame, dropping the oldest on overflow to bound latency.
func (v *viewer) offer(frame []byte) {
	select {
	case v.ch <- frame:
	default:
		select {
		case <-v.ch:
		default:
		}
		select {
		case v.ch <- frame:
		default:
		}
	}
}

// Multicast fans one camera reader out to many viewers.
type Multicast struct {
	mu      sync.Mutex
	cond    *sync.Cond
	viewers map[*viewer]struct{}
}

func NewMulticast() *Multicast {
	m := &Multicast{viewers: make(map[*viewer]struct{})}
	m.cond = sync.NewCond(&m.mu)
	return m
}

func (m *Multicast) add(v *viewer) {
	m.mu.Lock()
	m.viewers[v] = struct{}{}
	m.cond.Broadcast() // wake the streamer if it was idle
	m.mu.Unlock()
}

// WaitForViewer blocks until at least one viewer is connected.
func (m *Multicast) WaitForViewer() {
	m.mu.Lock()
	for len(m.viewers) == 0 {
		m.cond.Wait()
	}
	m.mu.Unlock()
}

func (m *Multicast) remove(v *viewer) {
	m.mu.Lock()
	delete(m.viewers, v)
	m.mu.Unlock()
}

func (m *Multicast) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.viewers)
}

func (m *Multicast) BroadcastVideo(ts uint64, au []byte, key bool) {
	var k byte
	if key {
		k = 1
	}
	frame := message(msgVideo, k, ts, au)
	m.mu.Lock()
	defer m.mu.Unlock()
	for v := range m.viewers {
		if v.waitingKey {
			if !key {
				continue
			}
			v.waitingKey = false
		}
		v.offer(frame)
	}
}

func (m *Multicast) BroadcastAudio(ts uint64, data []byte) {
	frame := message(msgAudio, 0, ts, data)
	m.mu.Lock()
	defer m.mu.Unlock()
	for v := range m.viewers {
		if !v.waitingKey {
			v.offer(frame)
		}
	}
}
