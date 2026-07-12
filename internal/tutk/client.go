// Package tutk is a thin cgo wrapper over the stock ThroughTek Kalay SDK
// (libIOTCAPIs_ALL.so): connect to a camera by UID + AuthKey and pull the raw
// A/V FIFOs. The proprietary P2P/DTLS stack stays in the vendor library.
package tutk

/*
#cgo LDFLAGS: -L/usr/local/lib -lIOTCAPIs_ALL
#include <stdlib.h>
#include "tutk.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Params are the connection inputs the SDK needs (kept independent of the app's
// config so this package has no dependency on the rest of the program).
type Params struct {
	UID            string
	AuthKey        string
	Account        string
	Password       string
	SDKKey         string
	Channel        int
	Region         int
	ConnectTimeout int
	LanSearchPort  int
}

const (
	videoBuf = 800000
	audioBuf = 16000
	infoBuf  = 4096
)

// AV recv result codes we branch on (from AVAPIs.h).
const (
	ErrTimeout              = -20011
	ErrDataNoACK            = -20012
	ErrIncompleteFrame      = -20013
	ErrLosedThisFrame       = -20014
	ErrSessionCloseByRemote = -20015
)

// Recoverable reports whether a negative recv code just means "poll again".
func Recoverable(rc int) bool {
	switch rc {
	case ErrTimeout, ErrDataNoACK, ErrIncompleteFrame, ErrLosedThisFrame:
		return true
	}
	return false
}

type FrameInfo struct {
	Codec  int
	Flags  int
	Width  int
	Height int
}

// Session is one connected camera: IOTC session + authenticated A/V channel.
type Session struct {
	av  C.int
	sid C.int

	vbuf, abuf, vinfo, ainfo unsafe.Pointer
}

// Open unlocks the SDK, connects by UID and authenticates the A/V channel.
func Open(cfg Params) (*Session, error) {
	if cfg.SDKKey != "" {
		key := C.CString(cfg.SDKKey)
		rc := C.TUTK_SDK_Set_License_Key(key)
		C.free(unsafe.Pointer(key))
		if rc < 0 {
			return nil, fmt.Errorf("set license key: %d", int(rc))
		}
		C.TUTK_SDK_Set_Region(C.int(cfg.Region))
	}

	if rc := C.IOTC_Initialize2(0); rc != 0 {
		return nil, fmt.Errorf("IOTC_Initialize2: %d", int(rc))
	}
	if cfg.LanSearchPort > 0 {
		C.IOTC_Set_LanSearchPort(C.int(cfg.LanSearchPort))
	}
	if rc := C.avInitialize(3); rc < 0 {
		deinit()
		return nil, fmt.Errorf("avInitialize: %d", int(rc))
	}

	sid := C.IOTC_Get_SessionID()
	if sid < 0 {
		deinit()
		return nil, fmt.Errorf("IOTC_Get_SessionID: %d", int(sid))
	}

	var in C.St_IOTCConnectInput
	in.cb = C.uint32_t(unsafe.Sizeof(in))
	ak := []byte(cfg.AuthKey)
	for i := 0; i < len(ak) && i < 8; i++ {
		in.auth_key[i] = C.char(ak[i])
	}
	in.timeout = C.uint32_t(cfg.ConnectTimeout)

	uid := C.CString(cfg.UID)
	rc := C.IOTC_Connect_ByUIDEx(uid, sid, &in)
	C.free(unsafe.Pointer(uid))
	if rc < 0 {
		deinit()
		return nil, fmt.Errorf("IOTC_Connect_ByUIDEx: %d", int(rc))
	}

	acct := C.CString(cfg.Account)
	pass := C.CString(cfg.Password)
	var ain C.AVClientStartInConfig
	ain.cb = C.uint32_t(unsafe.Sizeof(ain))
	ain.iotc_session_id = C.uint32_t(sid)
	ain.iotc_channel_id = C.uint8_t(cfg.Channel)
	ain.timeout_sec = 20
	ain.account_or_identity = acct
	ain.password_or_token = pass
	ain.resend = 1
	ain.security_mode = 2 // DTLS (auth-key firmware)
	var aout C.AVClientStartOutConfig
	aout.cb = C.uint32_t(unsafe.Sizeof(aout))
	av := C.avClientStartEx(&ain, &aout)
	C.free(unsafe.Pointer(acct))
	C.free(unsafe.Pointer(pass))
	if av < 0 {
		C.IOTC_Session_Close(sid)
		deinit()
		return nil, fmt.Errorf("avClientStartEx: %d", int(av))
	}

	return &Session{
		av:    av,
		sid:   sid,
		vbuf:  C.malloc(C.size_t(videoBuf)),
		abuf:  C.malloc(C.size_t(audioBuf)),
		vinfo: C.malloc(C.size_t(infoBuf)),
		ainfo: C.malloc(C.size_t(infoBuf)),
	}, nil
}

// StartStream requests a quality then starts the video (and audio) FIFOs.
func (s *Session) StartStream(quality, channel int, audio bool) {
	s.send(cmdSetStreamCtrl, setStreamCtrl(quality, channel))
	s.send(cmdStart, avStream(channel))
	if audio {
		s.send(cmdAudioStart, avStream(channel))
	}
}

func (s *Session) send(cmd int, payload []byte) {
	var p *C.char
	if len(payload) > 0 {
		p = (*C.char)(unsafe.Pointer(&payload[0]))
	}
	C.avSendIOCtrl(s.av, C.uint(cmd), p, C.int(len(payload)))
}

// RecvVideo pulls one H.264 access unit. rc < 0 is a status code (see Recoverable).
func (s *Session) RecvVideo() (rc int, data []byte, info FrameInfo) {
	var actual, expected, infoActual C.int
	var idx C.uint
	r := C.avRecvFrameData2(s.av,
		(*C.char)(s.vbuf), C.int(videoBuf), &actual, &expected,
		(*C.char)(s.vinfo), C.int(infoBuf), &infoActual, &idx)
	if r < 0 {
		return int(r), nil, FrameInfo{}
	}
	return int(r), C.GoBytes(s.vbuf, actual), readInfo(s.vinfo)
}

// RecvAudio pulls one audio frame (rc = byte count when >= 0).
func (s *Session) RecvAudio() (rc int, data []byte, info FrameInfo) {
	var idx C.uint
	r := C.avRecvAudioData(s.av,
		(*C.char)(s.abuf), C.int(audioBuf),
		(*C.char)(s.ainfo), C.int(infoBuf), &idx)
	if r < 0 {
		return int(r), nil, FrameInfo{}
	}
	return int(r), C.GoBytes(s.abuf, r), readInfo(s.ainfo)
}

// Close tears down the A/V channel, the IOTC session and the SDK.
func (s *Session) Close() {
	C.avSendIOCtrlExit(s.av)
	C.avClientStop(s.av)
	C.IOTC_Session_Close(s.sid)
	deinit()
	C.free(s.vbuf)
	C.free(s.abuf)
	C.free(s.vinfo)
	C.free(s.ainfo)
}

func readInfo(p unsafe.Pointer) FrameInfo {
	fi := (*C.FrameInfo)(p)
	return FrameInfo{
		Codec:  int(fi.codec_id),
		Flags:  int(fi.flags),
		Width:  int(fi.video_width),
		Height: int(fi.video_height),
	}
}

func deinit() {
	C.avDeInitialize()
	C.IOTC_DeInitialize()
}
