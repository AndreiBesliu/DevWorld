module github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording

go 1.27.2

require (
	filippo.io/age v1.3.2
	github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video v0.0.0
	github.com/bluenviron/gortsplib/v5 v5.6.6
	github.com/bluenviron/mediacommon/v2 v2.9.6
	github.com/pion/ice/v4 v4.4.5
	github.com/pion/rtp v1.10.5
	github.com/pion/webrtc/v4 v4.2.22
	golang.org/x/sys v0.48.0
)

require (
	filippo.io/hpke v0.4.0 // indirect
	github.com/abema/go-mp4 v1.7.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/pion/datachannel v1.6.3 // indirect
	github.com/pion/dtls/v3 v3.1.9 // indirect
	github.com/pion/interceptor v0.1.49 // indirect
	github.com/pion/logging v0.2.4 // indirect
	github.com/pion/mdns/v2 v2.2.2 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/rtcp v1.2.19 // indirect
	github.com/pion/sctp v1.11.3 // indirect
	github.com/pion/sdp/v3 v3.0.20 // indirect
	github.com/pion/srtp/v3 v3.1.0 // indirect
	github.com/pion/stun/v4 v4.0.1 // indirect
	github.com/pion/transport/v5 v5.1.1 // indirect
	github.com/pion/turn/v5 v5.1.2 // indirect
	github.com/wlynxg/anet v0.0.5 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/time v0.14.0 // indirect
)

replace github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video => ../video
