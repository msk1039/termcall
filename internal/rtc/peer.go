package rtc

import (
	"sync"

	"github.com/msk1039/termcall/internal/playback"
	"github.com/pion/webrtc/v4"
)

type RemotePeer struct {
	PeerID        string
	Username      string
	PC            *webrtc.PeerConnection
	DataChan      *webrtc.DataChannel // For ASCII frames
	ControlChan   *webrtc.DataChannel // For reliable control messages
	AudioTrack    *webrtc.TrackRemote // Incoming audio
	AudioSender   *webrtc.RTPSender   // Outgoing audio sender (for muting)
	Player        *playback.Player    // Playback engine
	LastFrame     string              // Latest ASCII frame for rendering
	Stats         PeerStats
	prevVideoSent uint64
	prevVideoRecv uint64
	prevAudioSent uint64
	prevAudioRecv uint64
	mu            sync.RWMutex
}

type PeerStats struct {
	OutgoingKBps    float64
	IncomingKBps    float64
	AudioOutKBps    float64
	AudioInKBps     float64
	PacketsLost     uint32
	Jitter          float64
	RTTMs           float64
	LocalCandidate  string
	RemoteCandidate string
	ConnectionState string
}

func (rp *RemotePeer) UpdateFrame(frame string) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	rp.LastFrame = frame
}

func (rp *RemotePeer) GetFrame() string {
	rp.mu.RLock()
	defer rp.mu.RUnlock()
	return rp.LastFrame
}

func (rp *RemotePeer) Close() {
	if rp.PC != nil {
		rp.PC.Close()
	}
}

func (rp *RemotePeer) UpdateStats(intervalSeconds float64) {
	if rp.PC == nil || rp.PC.ConnectionState() != webrtc.PeerConnectionStateConnected {
		return
	}

	stats := rp.PC.GetStats()
	rp.mu.Lock()
	defer rp.mu.Unlock()

	rp.Stats.ConnectionState = rp.PC.ConnectionState().String()

	for _, stat := range stats {
		switch s := stat.(type) {
		case webrtc.OutboundRTPStreamStats:
			if s.Kind == "video" {
				delta := s.BytesSent - rp.prevVideoSent
				rp.Stats.OutgoingKBps = float64(delta) / 1024.0 / intervalSeconds
				rp.prevVideoSent = s.BytesSent
			} else if s.Kind == "audio" {
				delta := s.BytesSent - rp.prevAudioSent
				rp.Stats.AudioOutKBps = float64(delta) / 1024.0 / intervalSeconds
				rp.prevAudioSent = s.BytesSent
			}
		case webrtc.InboundRTPStreamStats:
			if s.Kind == "video" {
				delta := s.BytesReceived - rp.prevVideoRecv
				rp.Stats.IncomingKBps = float64(delta) / 1024.0 / intervalSeconds
				rp.prevVideoRecv = s.BytesReceived
			} else if s.Kind == "audio" {
				delta := s.BytesReceived - rp.prevAudioRecv
				rp.Stats.AudioInKBps = float64(delta) / 1024.0 / intervalSeconds
				rp.prevAudioRecv = s.BytesReceived
				rp.Stats.PacketsLost = uint32(s.PacketsLost)
				rp.Stats.Jitter = s.Jitter
			}
		case webrtc.ICECandidatePairStats:
			if s.State == webrtc.StatsICECandidatePairStateSucceeded {
				rp.Stats.RTTMs = s.CurrentRoundTripTime * 1000.0
			}
		}
	}

	// DataChannels don't show up in RTPStats.
	// To get data channel stats we need to check DataChannelStats.
	for _, stat := range stats {
		if s, ok := stat.(webrtc.DataChannelStats); ok && s.Label == "video-ascii" {
			deltaSent := s.BytesSent - rp.prevVideoSent
			rp.Stats.OutgoingKBps = float64(deltaSent) / 1024.0 / intervalSeconds
			rp.prevVideoSent = s.BytesSent

			deltaRecv := s.BytesReceived - rp.prevVideoRecv
			rp.Stats.IncomingKBps = float64(deltaRecv) / 1024.0 / intervalSeconds
			rp.prevVideoRecv = s.BytesReceived
		}
	}
}
