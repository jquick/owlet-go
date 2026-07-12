package tutk

import "encoding/binary"

// AVIOCTRLDEF command ids.
const (
	cmdStart         = 0x01FF
	cmdAudioStart    = 0x0300
	cmdSetStreamCtrl = 0x0120
)

const (
	qualityMax    = 1
	qualityHigh   = 2
	qualityMiddle = 3
	qualityLow    = 4
	qualityMin    = 5
)

var qualityNames = map[string]int{
	"max": qualityMax,
	"high": qualityHigh, "hd": qualityHigh,
	"middle": qualityMiddle, "sd": qualityMiddle,
	"low": qualityLow, "ld": qualityLow,
	"min": qualityMin,
}

// QualityFromName maps a friendly name to an AV quality level (default high).
func QualityFromName(name string) int {
	if q, ok := qualityNames[name]; ok {
		return q
	}
	return qualityHigh
}

// avStream is SMsgAVIoctrlAVStream { uint32 channel; uint8 reserved[4]; }.
func avStream(channel int) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b[:4], uint32(channel))
	return b
}

// setStreamCtrl is SMsgAVIoctrlSetStreamCtrlReq { uint8 channel; uint8 quality; ... }.
func setStreamCtrl(quality, channel int) []byte {
	b := make([]byte, 8)
	b[0] = byte(channel)
	b[1] = byte(quality)
	return b
}
