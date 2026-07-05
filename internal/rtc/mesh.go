package rtc

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/msk1039/termcall/internal/playback"
	"github.com/msk1039/termcall/internal/protocol"
	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

type MeshManager struct {
	frameSeq    atomic.Uint64
	LocalPeerID string
	Peers       map[string]*RemotePeer
	mu          sync.RWMutex

	signaler   SignalTransport
	webrtcAPI  *webrtc.API
	iceServers []webrtc.ICEServer

	localAudioTrack webrtc.TrackLocal
	isMuted         bool

	// Callbacks
	OnFrame     func(peerID string, frame string)
	OnPeerJoin  func(peerID, username string)
	OnPeerLeave func(peerID string)

	// Context for graceful shutdown
	ctx    context.Context
	cancel context.CancelFunc
}

func NewMeshManager(signaler SignalTransport) *MeshManager {
	ctx, cancel := context.WithCancel(context.Background())

	// Prepare MediaEngine (will add Opus later)
	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		log.Fatalf("Failed to register codecs: %v", err)
	}

	s := webrtc.SettingEngine{}
	s.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	api := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithSettingEngine(s))

	return &MeshManager{
		Peers:     make(map[string]*RemotePeer),
		signaler:  signaler,
		webrtcAPI: api,
		ctx:       ctx,
		cancel:    cancel,
	}
}

func (m *MeshManager) Start() {
	go m.readSignalingLoop()
}

func (m *MeshManager) Stop() {
	m.cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.Peers {
		p.Close()
	}
}

// RLockPeers allows safe read-only access to the Peers map for diagnostics.
func (m *MeshManager) RLockPeers(fn func(peers map[string]*RemotePeer)) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	fn(m.Peers)
}

func (m *MeshManager) SetICEServers(servers []webrtc.ICEServer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.iceServers = servers
}

func (m *MeshManager) SetLocalAudioTrack(track webrtc.TrackLocal) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.localAudioTrack = track
}

// SetMuteAudio mutes or unmutes the local audio sent to all peers.
func (m *MeshManager) SetMuteAudio(muted bool) {
	m.mu.Lock()
	m.isMuted = muted
	localTrack := m.localAudioTrack
	m.mu.Unlock()

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, p := range m.Peers {
		if p.AudioSender != nil {
			if muted {
				_ = p.AudioSender.ReplaceTrack(nil)
			} else {
				_ = p.AudioSender.ReplaceTrack(localTrack)
			}
		}
	}
}

// maxBufferedAmount is the maximum number of bytes allowed in the DataChannel's
// SCTP send buffer before we start dropping frames. This prevents buffer bloat
// when the network is congested — without it, frames pile up during congestion
// and replay in a burst when the network recovers.
const maxBufferedAmount = 64 * 1024 // 64 KB

func (m *MeshManager) BroadcastFrame(frame []byte) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Tag the frame with a monotonically increasing sequence number so the
	// receiver can drop stale frames that arrive out of order.
	seq := m.frameSeq.Add(1)
	tagged := fmt.Sprintf("%d\n%s", seq, string(frame))

	for _, p := range m.Peers {
		if p.DataChan == nil || p.DataChan.ReadyState() != webrtc.DataChannelStateOpen {
			continue
		}
		// Drop the frame if the send buffer is backed up for this peer. Queued
		// frames arrive late and cause a "replay" effect on the receiver, so
		// skipping is better than piling on more data.
		if p.DataChan.BufferedAmount() > maxBufferedAmount {
			continue
		}
		if err := p.DataChan.SendText(tagged); err != nil {
			// Send errors are non-fatal for individual frames.
		}
	}
}

// parseTaggedFrame splits a "<seq>\n<frame>" message into its sequence number
// and frame data. Returns ok=false if the message is malformed.
func parseTaggedFrame(raw string) (seq uint64, data string, ok bool) {
	idx := strings.IndexByte(raw, '\n')
	if idx < 0 {
		return 0, "", false
	}
	s, err := strconv.ParseUint(raw[:idx], 10, 64)
	if err != nil {
		return 0, "", false
	}
	return s, raw[idx+1:], true
}

func (m *MeshManager) readSignalingLoop() {
	for {
		select {
		case <-m.ctx.Done():
			return
		case msg, ok := <-m.signaler.Receive():
			if !ok {
				return
			}
			m.handleSignalingMessage(msg)
		}
	}
}

func (m *MeshManager) handleSignalingMessage(msg protocol.SignalingMessage) {
	switch msg.Type {
	case protocol.MsgRoomCreated:
		m.mu.Lock()
		m.LocalPeerID = msg.PeerID
		m.mu.Unlock()
		log.Printf("Joined room %s as %s", msg.RoomID, msg.PeerID)

	case protocol.MsgTURNCredentials:
		var creds protocol.TURNCredentials
		if err := json.Unmarshal(msg.Payload, &creds); err == nil {
			log.Printf("[ICE] Received TURN credentials: URLs=%v, Username=%s", creds.URLs, creds.Username)
			m.SetICEServers([]webrtc.ICEServer{
				{
					URLs:       creds.URLs,
					Username:   creds.Username,
					Credential: creds.Credential,
				},
			})
		} else {
			log.Printf("[ICE] Failed to parse TURN credentials: %v", err)
		}

	case protocol.MsgPeerJoined:
		log.Printf("[SIGNAL] Received PeerJoined: peerID=%s, username=%s", msg.PeerID, msg.Username)
		m.handlePeerJoined(msg)

	case protocol.MsgPeerLeft:
		log.Printf("[SIGNAL] Received PeerLeft: peerID=%s", msg.PeerID)
		m.handlePeerLeft(msg.PeerID)

	case protocol.MsgOffer:
		log.Printf("[SIGNAL] Received Offer from peerID=%s", msg.PeerID)
		m.handleOffer(msg)

	case protocol.MsgAnswer:
		log.Printf("[SIGNAL] Received Answer from peerID=%s", msg.PeerID)
		m.handleAnswer(msg)

	case protocol.MsgICECandidate:
		log.Printf("[SIGNAL] Received ICE Candidate from peerID=%s", msg.PeerID)
		m.handleICECandidate(msg)

	default:
		log.Printf("[SIGNAL] Unknown message type: %s", msg.Type)
	}
}

func (m *MeshManager) createPeerConnection(peerID string) (*webrtc.PeerConnection, *webrtc.RTPSender, error) {
	m.mu.RLock()
	iceServers := m.iceServers
	m.mu.RUnlock()

	log.Printf("[ICE] Creating PeerConnection for %s with %d ICE servers", peerID, len(iceServers))
	for _, s := range iceServers {
		log.Printf("[ICE]   Server URLs: %v", s.URLs)
	}

	config := webrtc.Configuration{
		ICEServers: iceServers,
	}

	pc, err := m.webrtcAPI.NewPeerConnection(config)
	if err != nil {
		return nil, nil, err
	}

	m.mu.RLock()
	localTrack := m.localAudioTrack
	muted := m.isMuted
	m.mu.RUnlock()

	var audioSender *webrtc.RTPSender
	if localTrack != nil {
		if sender, err := pc.AddTrack(localTrack); err != nil {
			log.Printf("Failed to add local audio track: %v", err)
		} else {
			audioSender = sender
			if muted {
				_ = audioSender.ReplaceTrack(nil)
			}
		}
	}

	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("Received %s track from %s", track.Kind().String(), peerID)
		if track.Kind() == webrtc.RTPCodecTypeAudio {
			player, err := playback.NewPlayer()
			if err != nil {
				log.Printf("Failed to create audio player for %s: %v", peerID, err)
				return
			}
			m.mu.Lock()
			if p, ok := m.Peers[peerID]; ok {
				p.Player = player
			}
			m.mu.Unlock()

			go func() {
				defer player.Close()
				for {
					packet, _, err := track.ReadRTP()
					if err != nil {
						return
					}
					if err := player.WriteOpus(packet.Payload); err != nil {
						// Silence decode errors, as packet loss can corrupt frames
					}
				}
			}()
		}
	})

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			log.Printf("[ICE] ICE gathering complete for peer %s", peerID)
			return
		}

		log.Printf("[ICE] Generated candidate for %s: type=%s addr=%s:%d", peerID, c.Typ.String(), c.Address, c.Port)

		candJSON := c.ToJSON()

		payload, _ := json.Marshal(protocol.ICECandidatePayload{
			Candidate:        candJSON.Candidate,
			SDPMid:           candJSON.SDPMid,
			SDPMLineIndex:    candJSON.SDPMLineIndex,
			UsernameFragment: candJSON.UsernameFragment,
		})

		log.Printf("[ICE] Sending candidate to %s via signaling", peerID)
		if err := m.signaler.Send(protocol.SignalingMessage{
			Type:    protocol.MsgICECandidate,
			PeerID:  peerID,
			Payload: payload,
		}); err != nil {
			log.Printf("[ICE] ERROR sending candidate to %s: %v", peerID, err)
		}
	})

	pc.OnICEConnectionStateChange(func(s webrtc.ICEConnectionState) {
		log.Printf("[ICE] ICE connection state for %s: %s", peerID, s.String())
	})

	pc.OnICEGatheringStateChange(func(s webrtc.ICEGatheringState) {
		log.Printf("[ICE] ICE gathering state for %s: %s", peerID, s.String())
	})

	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		log.Printf("Peer %s state changed: %s", peerID, s.String())
		if s == webrtc.PeerConnectionStateFailed || s == webrtc.PeerConnectionStateClosed || s == webrtc.PeerConnectionStateDisconnected {
			m.handlePeerLeft(peerID)
		}
	})

	return pc, audioSender, nil
}

func (m *MeshManager) handlePeerJoined(msg protocol.SignalingMessage) {
	peerID := msg.PeerID

	m.mu.RLock()
	_, exists := m.Peers[peerID]
	m.mu.RUnlock()
	if exists {
		log.Printf("[SIGNAL] Peer %s already exists, skipping", peerID)
		return // Already have this peer
	}

	pc, sender, err := m.createPeerConnection(peerID)
	if err != nil {
		log.Printf("Failed to create PC for answer: %v", err)
		return
	}

	peer := &RemotePeer{
		PeerID:      peerID,
		Username:    msg.Username,
		PC:          pc,
		AudioSender: sender,
	}

	m.mu.Lock()
	m.Peers[peerID] = peer
	m.mu.Unlock()

	if m.OnPeerJoin != nil {
		m.OnPeerJoin(peerID, peer.Username)
	}

	// Only initiate offer if LocalPeerID < peerID to avoid glare
	m.mu.RLock()
	localID := m.LocalPeerID
	m.mu.RUnlock()

	log.Printf("[SIGNAL] Glare check: localID=%s, peerID=%s, localID>peerID=%v", localID, peerID, localID > peerID)

	if localID > peerID {
		log.Printf("[SIGNAL] Waiting for offer from %s (we are the polite side)", peerID)
		pc.OnDataChannel(func(dc *webrtc.DataChannel) {
			log.Printf("[DATA] Received data channel '%s' from %s", dc.Label(), peerID)
			if dc.Label() == "video-ascii" {
				peer.DataChan = dc
				dc.OnMessage(func(dMsg webrtc.DataChannelMessage) {
					if !dMsg.IsString {
						return
					}
					seq, frame, ok := parseTaggedFrame(string(dMsg.Data))
					if !ok {
						return
					}
					if !peer.CompareAndUpdateSeq(seq) {
						return
					}
					peer.UpdateFrame(frame)
					if m.OnFrame != nil {
						m.OnFrame(peerID, frame)
					}
				})
			}
		})
		return
	}

	log.Printf("[SIGNAL] Creating offer for %s (we are the impolite side)", peerID)

	// Create unordered, unreliable datachannel for video frames
	maxRetransmits := uint16(0)
	ordered := false
	dc, err := pc.CreateDataChannel("video-ascii", &webrtc.DataChannelInit{
		MaxRetransmits: &maxRetransmits,
		Ordered:        &ordered,
	})
	if err != nil {
		log.Printf("Failed to create data channel: %v", err)
		return
	}
	peer.DataChan = dc

	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if !msg.IsString {
			return
		}
		seq, frame, ok := parseTaggedFrame(string(msg.Data))
		if !ok {
			return
		}
		if !peer.CompareAndUpdateSeq(seq) {
			return
		}
		peer.UpdateFrame(frame)
		if m.OnFrame != nil {
			m.OnFrame(peerID, frame)
		}
	})

	// Create offer
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		log.Printf("Failed to create offer: %v", err)
		return
	}

	if err = pc.SetLocalDescription(offer); err != nil {
		log.Printf("Failed to set local description: %v", err)
		return
	}

	log.Printf("[SIGNAL] Sending offer to %s (SDP length=%d)", peerID, len(offer.SDP))

	payload, _ := json.Marshal(protocol.SDPPayload{
		Type: "offer",
		SDP:  offer.SDP,
	})

	if err := m.signaler.Send(protocol.SignalingMessage{
		Type:    protocol.MsgOffer,
		PeerID:  peerID,
		Payload: payload,
	}); err != nil {
		log.Printf("[SIGNAL] ERROR sending offer to %s: %v", peerID, err)
	}
}

func (m *MeshManager) handleOffer(msg protocol.SignalingMessage) {
	peerID := msg.PeerID

	m.mu.RLock()
	peer, exists := m.Peers[peerID]
	m.mu.RUnlock()

	if !exists {
		log.Printf("Received offer from unknown peer %s", peerID)
		return
	}

	var offerPayload protocol.SDPPayload
	if err := json.Unmarshal(msg.Payload, &offerPayload); err != nil {
		log.Printf("[SIGNAL] ERROR unmarshalling offer from %s: %v", peerID, err)
		return
	}

	log.Printf("[SIGNAL] Setting remote offer from %s (SDP length=%d)", peerID, len(offerPayload.SDP))

	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offerPayload.SDP,
	}

	if err := peer.PC.SetRemoteDescription(offer); err != nil {
		log.Printf("Failed to set remote desc: %v", err)
		return
	}

	answer, err := peer.PC.CreateAnswer(nil)
	if err != nil {
		log.Printf("Failed to create answer: %v", err)
		return
	}

	if err := peer.PC.SetLocalDescription(answer); err != nil {
		log.Printf("Failed to set local desc: %v", err)
		return
	}

	log.Printf("[SIGNAL] Sending answer to %s (SDP length=%d)", peerID, len(answer.SDP))

	payload, _ := json.Marshal(protocol.SDPPayload{
		Type: "answer",
		SDP:  answer.SDP,
	})

	if err := m.signaler.Send(protocol.SignalingMessage{
		Type:    protocol.MsgAnswer,
		PeerID:  peerID,
		Payload: payload,
	}); err != nil {
		log.Printf("[SIGNAL] ERROR sending answer to %s: %v", peerID, err)
	}
}

func (m *MeshManager) handleAnswer(msg protocol.SignalingMessage) {
	m.mu.RLock()
	peer, ok := m.Peers[msg.PeerID]
	m.mu.RUnlock()

	if !ok {
		log.Printf("[SIGNAL] Received answer from unknown peer %s", msg.PeerID)
		return
	}

	var answerPayload protocol.SDPPayload
	if err := json.Unmarshal(msg.Payload, &answerPayload); err != nil {
		log.Printf("[SIGNAL] ERROR unmarshalling answer from %s: %v", msg.PeerID, err)
		return
	}

	log.Printf("[SIGNAL] Setting remote answer from %s (SDP length=%d)", msg.PeerID, len(answerPayload.SDP))

	answer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answerPayload.SDP,
	}

	if err := peer.PC.SetRemoteDescription(answer); err != nil {
		log.Printf("Failed to set remote desc: %v", err)
	} else {
		log.Printf("[SIGNAL] Successfully set remote answer from %s", msg.PeerID)
	}
}

func (m *MeshManager) handleICECandidate(msg protocol.SignalingMessage) {
	m.mu.RLock()
	peer, ok := m.Peers[msg.PeerID]
	m.mu.RUnlock()

	if !ok {
		log.Printf("[ICE] Received candidate from unknown peer %s, ignoring", msg.PeerID)
		return
	}

	var candPayload protocol.ICECandidatePayload
	if err := json.Unmarshal(msg.Payload, &candPayload); err != nil {
		log.Printf("[ICE] ERROR unmarshalling candidate from %s: %v", msg.PeerID, err)
		return
	}

	log.Printf("[ICE] Adding remote candidate from %s: %s", msg.PeerID, candPayload.Candidate)

	cand := webrtc.ICECandidateInit{
		Candidate:        candPayload.Candidate,
		SDPMid:           candPayload.SDPMid,
		SDPMLineIndex:    candPayload.SDPMLineIndex,
		UsernameFragment: candPayload.UsernameFragment,
	}

	if err := peer.PC.AddICECandidate(cand); err != nil {
		log.Printf("[ICE] ERROR adding candidate from %s: %v", msg.PeerID, err)
	} else {
		log.Printf("[ICE] Successfully added candidate from %s", msg.PeerID)
	}
}

func (m *MeshManager) handlePeerLeft(peerID string) {
	m.mu.Lock()
	peer, ok := m.Peers[peerID]
	if ok {
		delete(m.Peers, peerID)
	}
	m.mu.Unlock()

	if ok {
		peer.Close()
		if m.OnPeerLeave != nil {
			m.OnPeerLeave(peerID)
		}
	}
}
