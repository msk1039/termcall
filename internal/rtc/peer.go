package rtc

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/msk1039/termcall/internal/playback"
	"github.com/pion/webrtc/v4"
	"golang.org/x/time/rate"
)

const maxPendingICECandidates = 256

const maxRemoteFrameRate = rate.Limit(30)

var ErrPeerClosed = errors.New("remote peer is closed")

type RemotePeer struct {
	lastSeq  atomic.Uint64
	PeerID   string
	Username string
	PC       *webrtc.PeerConnection

	AudioSender *webrtc.RTPSender
	AudioTrack  *webrtc.TrackRemote

	dataChannel          *webrtc.DataChannel
	player               *playback.Player
	lastFrame            string
	stats                PeerStats
	pendingICE           []webrtc.ICECandidateInit
	remoteDescriptionSet bool
	prevVideoSent        uint64
	prevVideoRecv        uint64
	prevAudioSent        uint64
	prevAudioRecv        uint64
	prevDataSent         uint64
	prevDataRecv         uint64
	mu                   sync.RWMutex
	closeOnce            sync.Once
	closed               bool
	frameLimiter         *rate.Limiter
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

func (peer *RemotePeer) SetDataChannel(channel *webrtc.DataChannel) bool {
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if peer.closed {
		return false
	}
	peer.dataChannel = channel
	return true
}

func (peer *RemotePeer) DataChannel() *webrtc.DataChannel {
	peer.mu.RLock()
	defer peer.mu.RUnlock()
	return peer.dataChannel
}

func (peer *RemotePeer) SetPlayer(player *playback.Player) (*playback.Player, bool) {
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if peer.closed {
		return nil, false
	}
	previous := peer.player
	peer.player = player
	return previous, true
}

func (peer *RemotePeer) ClearPlayer(player *playback.Player) {
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if peer.player == player {
		peer.player = nil
	}
}

func (peer *RemotePeer) Player() *playback.Player {
	peer.mu.RLock()
	defer peer.mu.RUnlock()
	return peer.player
}

func (peer *RemotePeer) UpdateFrame(frame string) {
	peer.mu.Lock()
	defer peer.mu.Unlock()
	peer.lastFrame = frame
}

func (peer *RemotePeer) GetFrame() string {
	peer.mu.RLock()
	defer peer.mu.RUnlock()
	return peer.lastFrame
}

func (peer *RemotePeer) StatsSnapshot() PeerStats {
	peer.mu.RLock()
	defer peer.mu.RUnlock()
	return peer.stats
}

func (peer *RemotePeer) CompareAndUpdateSeq(sequence uint64) bool {
	for {
		oldSequence := peer.lastSeq.Load()
		if sequence <= oldSequence {
			return false
		}
		if peer.lastSeq.CompareAndSwap(oldSequence, sequence) {
			return true
		}
	}
}

func (peer *RemotePeer) AllowFrame() bool {
	peer.mu.Lock()
	if peer.frameLimiter == nil {
		peer.frameLimiter = rate.NewLimiter(maxRemoteFrameRate, 2)
	}
	limiter := peer.frameLimiter
	closed := peer.closed
	peer.mu.Unlock()
	return !closed && limiter.Allow()
}

func (peer *RemotePeer) AddICECandidate(candidate webrtc.ICECandidateInit) error {
	peer.mu.Lock()
	if peer.closed {
		peer.mu.Unlock()
		return ErrPeerClosed
	}
	if !peer.remoteDescriptionSet {
		if len(peer.pendingICE) >= maxPendingICECandidates {
			peer.mu.Unlock()
			return fmt.Errorf("too many pending ICE candidates (maximum %d)", maxPendingICECandidates)
		}
		peer.pendingICE = append(peer.pendingICE, candidate)
		peer.mu.Unlock()
		return nil
	}
	peer.mu.Unlock()
	return peer.PC.AddICECandidate(candidate)
}

func (peer *RemotePeer) SetRemoteDescription(description webrtc.SessionDescription) error {
	peer.mu.RLock()
	closed := peer.closed
	connection := peer.PC
	peer.mu.RUnlock()
	if closed {
		return ErrPeerClosed
	}
	if connection == nil {
		return errors.New("remote peer has no peer connection")
	}
	if err := connection.SetRemoteDescription(description); err != nil {
		return err
	}

	peer.mu.Lock()
	peer.remoteDescriptionSet = true
	pending := append([]webrtc.ICECandidateInit{}, peer.pendingICE...)
	peer.pendingICE = []webrtc.ICECandidateInit{}
	peer.mu.Unlock()

	var candidateErrors []error
	for _, candidate := range pending {
		if err := connection.AddICECandidate(candidate); err != nil {
			candidateErrors = append(candidateErrors, err)
		}
	}
	return errors.Join(candidateErrors...)
}

func (peer *RemotePeer) Close() {
	peer.closeOnce.Do(func() {
		peer.mu.Lock()
		peer.closed = true
		player := peer.player
		peer.player = nil
		connection := peer.PC
		peer.pendingICE = nil
		peer.mu.Unlock()
		if player != nil {
			player.Close()
		}
		if connection != nil {
			_ = connection.Close()
		}
	})
}

func (peer *RemotePeer) UpdateStats(intervalSeconds float64) {
	if peer.PC == nil || peer.PC.ConnectionState() != webrtc.PeerConnectionStateConnected {
		return
	}
	if intervalSeconds <= 0 {
		return
	}

	stats := peer.PC.GetStats()
	peer.mu.Lock()
	defer peer.mu.Unlock()
	peer.stats.ConnectionState = peer.PC.ConnectionState().String()

	for _, stat := range stats {
		switch typed := stat.(type) {
		case webrtc.OutboundRTPStreamStats:
			switch typed.Kind {
			case "video":
				delta := counterDelta(typed.BytesSent, peer.prevVideoSent)
				peer.stats.OutgoingKBps = bytesPerSecondToKBps(delta, intervalSeconds)
				peer.prevVideoSent = typed.BytesSent
			case "audio":
				delta := counterDelta(typed.BytesSent, peer.prevAudioSent)
				peer.stats.AudioOutKBps = bytesPerSecondToKBps(delta, intervalSeconds)
				peer.prevAudioSent = typed.BytesSent
			}
		case webrtc.InboundRTPStreamStats:
			switch typed.Kind {
			case "video":
				delta := counterDelta(typed.BytesReceived, peer.prevVideoRecv)
				peer.stats.IncomingKBps = bytesPerSecondToKBps(delta, intervalSeconds)
				peer.prevVideoRecv = typed.BytesReceived
			case "audio":
				delta := counterDelta(typed.BytesReceived, peer.prevAudioRecv)
				peer.stats.AudioInKBps = bytesPerSecondToKBps(delta, intervalSeconds)
				peer.prevAudioRecv = typed.BytesReceived
				peer.stats.PacketsLost = 0
				if typed.PacketsLost > 0 {
					peer.stats.PacketsLost = uint32(typed.PacketsLost)
				}
				peer.stats.Jitter = typed.Jitter
			}
		case webrtc.ICECandidatePairStats:
			if typed.State == webrtc.StatsICECandidatePairStateSucceeded {
				peer.stats.RTTMs = typed.CurrentRoundTripTime * 1000
			}
		case webrtc.DataChannelStats:
			if typed.Label != "video-ascii" {
				continue
			}
			deltaSent := counterDelta(typed.BytesSent, peer.prevDataSent)
			peer.stats.OutgoingKBps = bytesPerSecondToKBps(deltaSent, intervalSeconds)
			peer.prevDataSent = typed.BytesSent

			deltaReceived := counterDelta(typed.BytesReceived, peer.prevDataRecv)
			peer.stats.IncomingKBps = bytesPerSecondToKBps(deltaReceived, intervalSeconds)
			peer.prevDataRecv = typed.BytesReceived
		}
	}
}

func bytesPerSecondToKBps(bytes uint64, seconds float64) float64 {
	return float64(bytes) / 1024 / seconds
}

func counterDelta(current, previous uint64) uint64 {
	if current < previous {
		return current
	}
	return current - previous
}
