package owletcam

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"time"
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// App-level message kinds (first header byte).
const (
	msgVideo = 0
	msgAudio = 1
)

func wsAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// message builds one unmasked binary WebSocket frame whose body is:
//
//	kind(u8) key(u8) ts_micros(u64 BE) wall_ms(u64 BE) | raw payload
//
// wall_ms is the server's wall-clock (ms since epoch) at send time -- the
// authoritative timestamp painted onto the video, independent of the client.
func message(kind, key byte, ts uint64, payload []byte) []byte {
	body := make([]byte, 18+len(payload))
	body[0] = kind
	body[1] = key
	binary.BigEndian.PutUint64(body[2:10], ts)
	binary.BigEndian.PutUint64(body[10:18], uint64(time.Now().UnixMilli()))
	copy(body[18:], payload)

	n := len(body)
	switch {
	case n < 126:
		return append([]byte{0x82, byte(n)}, body...)
	case n < 65536:
		return append([]byte{0x82, 126, byte(n >> 8), byte(n)}, body...)
	default:
		h := make([]byte, 10)
		h[0], h[1] = 0x82, 127
		binary.BigEndian.PutUint64(h[2:], uint64(n))
		return append(h, body...)
	}
}
