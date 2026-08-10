package rtc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/msk1039/termcall/internal/ascii"
	"github.com/msk1039/termcall/internal/playback"
	"github.com/msk1039/termcall/internal/protocol"
	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
	"golang.org/x/time/rate"
)

const (
	maxBufferedAmount     = 64 * 1024
	disconnectGracePeriod = 10 * time.Second
)

type MeshManager struct {
	frameSeq    atomic.Uint64
	LocalPeerID string
	Peers       map[string]*RemotePeer
	mu          sync.RWMutex

	signaler     SignalTransport
	webrtcAPI    *webrtc.API
	iceServers   []webrtc.ICEServer
	initialPeers []protocol.PeerInfo

	localAudioTrack  webrtc.TrackLocal
	silentAudioTrack webrtc.TrackLocal
	isMuted          bool

	OnFrame     func(peerID string, frame string)
	OnPeerJoin  func(peerID, username string)
	OnPeerLeave func(peerID string)
	OnError     func(error)

	ctx       context.Context
	cancel    context.CancelFunc
	startOnce sync.Once
	stopOnce  sync.Once
	wg        sync.WaitGroup
}

type MeshOptions struct {
	AllowPrivateCandidates bool
}

func NewMeshManager(
	parent context.Context,
	signaler SignalTransport,
	admission protocol.JoinAcceptedPayload,
) (*MeshManager, error) {
	return NewMeshManagerWithOptions(parent, signaler, admission, MeshOptions{})
}

func NewMeshManagerWithOptions(
	parent context.Context,
	signaler SignalTransport,
	admission protocol.JoinAcceptedPayload,
	options MeshOptions,
) (*MeshManager, error) {
	if signaler == nil {
		return nil, errors.New("signaling transport is required")
	}
	if err := protocol.ValidatePeerID(admission.PeerID); err != nil {
		return nil, fmt.Errorf("local peer ID: %w", err)
	}

	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, fmt.Errorf("register WebRTC codecs: %w", err)
	}
	settingEngine := webrtc.SettingEngine{}
	settingEngine.SetICEMulticastDNSMode(ice.MulticastDNSModeQueryAndGather)
	settingEngine.SetRemoteIPFilter(func(ip net.IP) bool {
		return remoteCandidateAllowedWithPrivate(ip, options.AllowPrivateCandidates)
	})
	settingEngine.SetSCTPMaxMessageSize(uint32(ascii.MaxEncodedFrameBytes + 21))
	settingEngine.SetSCTPMaxReceiveBufferSize(1024 * 1024)
	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithSettingEngine(settingEngine),
	)
	silentAudioTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeOpus,
			ClockRate: 48000,
			Channels:  2,
		},
		"audio",
		"termcall",
	)
	if err != nil {
		return nil, fmt.Errorf("create silent audio track: %w", err)
	}

	iceServers := make([]webrtc.ICEServer, 0, len(admission.ICEServers))
	for _, server := range admission.ICEServers {
		iceServers = append(iceServers, webrtc.ICEServer{
			URLs:       append([]string{}, server.URLs...),
			Username:   server.Username,
			Credential: server.Credential,
		})
	}
	ctx, cancel := context.WithCancel(parent)
	return &MeshManager{
		LocalPeerID: admission.PeerID,
		Peers:       map[string]*RemotePeer{},
		signaler:    signaler,
		webrtcAPI:   api,
		iceServers:  iceServers,
		initialPeers: append(
			[]protocol.PeerInfo{},
			admission.Peers...,
		),
		silentAudioTrack: silentAudioTrack,
		isMuted:          true,
		ctx:              ctx,
		cancel:           cancel,
	}, nil
}

func (manager *MeshManager) Start() {
	manager.startOnce.Do(func() {
		for _, peer := range manager.initialPeers {
			message := protocol.NewMessage(protocol.MsgPeerJoined)
			message.PeerID = peer.PeerID
			message.Username = peer.Username
			manager.handlePeerJoined(message)
		}
		manager.initialPeers = []protocol.PeerInfo{}

		manager.wg.Add(1)
		go func() {
			defer manager.wg.Done()
			manager.readSignalingLoop()
		}()
	})
}

func (manager *MeshManager) Stop() {
	manager.stopOnce.Do(func() {
		manager.cancel()
		_ = manager.signaler.Close()

		manager.mu.Lock()
		peers := make([]*RemotePeer, 0, len(manager.Peers))
		for peerID, peer := range manager.Peers {
			peers = append(peers, peer)
			delete(manager.Peers, peerID)
		}
		manager.mu.Unlock()

		for _, peer := range peers {
			peer.Close()
		}
		manager.wg.Wait()
	})
}

func (manager *MeshManager) RLockPeers(callback func(peers map[string]*RemotePeer)) {
	manager.mu.RLock()
	snapshot := make(map[string]*RemotePeer, len(manager.Peers))
	for peerID, peer := range manager.Peers {
		snapshot[peerID] = peer
	}
	manager.mu.RUnlock()
	callback(snapshot)
}

func (manager *MeshManager) SetLocalAudioTrack(track webrtc.TrackLocal) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.localAudioTrack = track
}

func (manager *MeshManager) SetMuteAudio(muted bool) {
	manager.mu.Lock()
	manager.isMuted = muted
	localTrack := manager.localAudioTrack
	if muted || localTrack == nil {
		localTrack = manager.silentAudioTrack
	}
	peers := make([]*RemotePeer, 0, len(manager.Peers))
	for _, peer := range manager.Peers {
		peers = append(peers, peer)
	}
	manager.mu.Unlock()

	for _, peer := range peers {
		if peer.AudioSender == nil {
			continue
		}
		if err := peer.AudioSender.ReplaceTrack(localTrack); err != nil {
			manager.notifyError(fmt.Errorf("update audio track for peer %s: %w", peer.PeerID, err))
		}
	}
}

func (manager *MeshManager) BroadcastFrame(frame []byte) {
	if len(frame) == 0 || len(frame) > ascii.MaxEncodedFrameBytes {
		return
	}
	sequence := manager.frameSeq.Add(1)
	tagged := strconv.FormatUint(sequence, 10) + "\n" + string(frame)

	manager.mu.RLock()
	peers := make([]*RemotePeer, 0, len(manager.Peers))
	for _, peer := range manager.Peers {
		peers = append(peers, peer)
	}
	manager.mu.RUnlock()

	for _, peer := range peers {
		channel := peer.DataChannel()
		if channel == nil || channel.ReadyState() != webrtc.DataChannelStateOpen {
			continue
		}
		if channel.BufferedAmount() > maxBufferedAmount {
			continue
		}
		_ = channel.SendText(tagged)
	}
}

func parseTaggedFrame(raw []byte) (uint64, string, bool) {
	if len(raw) > ascii.MaxEncodedFrameBytes+21 {
		return 0, "", false
	}
	newline := bytes.IndexByte(raw, '\n')
	if newline < 1 || newline > 20 {
		return 0, "", false
	}
	sequence, err := strconv.ParseUint(string(raw[:newline]), 10, 64)
	if err != nil || sequence == 0 {
		return 0, "", false
	}
	frame := raw[newline+1:]
	if len(frame) == 0 || len(frame) > ascii.MaxEncodedFrameBytes {
		return 0, "", false
	}
	return sequence, string(frame), true
}

var deniedRemoteCandidatePrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func remoteCandidateAllowed(ip net.IP) bool {
	return remoteCandidateAllowedWithPrivate(ip, false)
}

func remoteCandidateAllowedWithPrivate(ip net.IP, allowPrivate bool) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	if address.IsPrivate() {
		return allowPrivate
	}
	for _, prefix := range deniedRemoteCandidatePrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func (manager *MeshManager) readSignalingLoop() {
	messages := manager.signaler.Receive()
	errorsChannel := manager.signaler.Errors()
	for messages != nil || errorsChannel != nil {
		select {
		case <-manager.ctx.Done():
			return
		case err, ok := <-errorsChannel:
			if !ok {
				errorsChannel = nil
				continue
			}
			if err != nil {
				manager.notifyError(err)
			}
		case message, ok := <-messages:
			if !ok {
				messages = nil
				if manager.ctx.Err() == nil {
					manager.notifyError(errors.New("signaling connection closed"))
				}
				continue
			}
			manager.handleSignalingMessage(message)
		}
	}
}

func (manager *MeshManager) handleSignalingMessage(message protocol.SignalingMessage) {
	switch message.Type {
	case protocol.MsgPeerJoined:
		manager.handlePeerJoined(message)
	case protocol.MsgPeerLeft:
		manager.handlePeerLeft(message.PeerID)
	case protocol.MsgOffer:
		manager.handleOffer(message)
	case protocol.MsgAnswer:
		manager.handleAnswer(message)
	case protocol.MsgICECandidate:
		manager.handleICECandidate(message)
	case protocol.MsgError:
		failure, err := protocol.DecodePayload[protocol.ErrorPayload](message.Payload)
		if err != nil {
			manager.notifyError(err)
			return
		}
		manager.notifyError(fmt.Errorf("signaling error %s: %s", failure.Code, failure.Message))
	default:
		manager.notifyError(fmt.Errorf("unexpected signaling message %q", message.Type))
	}
}

func (manager *MeshManager) createPeerConnection(peerID string) (*webrtc.PeerConnection, *webrtc.RTPSender, error) {
	manager.mu.RLock()
	iceServers := append([]webrtc.ICEServer{}, manager.iceServers...)
	localTrack := manager.localAudioTrack
	muted := manager.isMuted
	if muted || localTrack == nil {
		localTrack = manager.silentAudioTrack
	}
	manager.mu.RUnlock()

	peerConnection, err := manager.webrtcAPI.NewPeerConnection(webrtc.Configuration{
		ICEServers: iceServers,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("create WebRTC peer connection: %w", err)
	}

	var audioSender *webrtc.RTPSender
	if localTrack != nil {
		audioSender, err = peerConnection.AddTrack(localTrack)
		if err != nil {
			_ = peerConnection.Close()
			return nil, nil, fmt.Errorf("add local audio track: %w", err)
		}
	}

	peerConnection.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if track.Kind() != webrtc.RTPCodecTypeAudio {
			return
		}
		player, err := playback.NewPlayer()
		if err != nil {
			manager.notifyError(fmt.Errorf("create audio player for peer %s: %w", peerID, err))
			return
		}
		peer := manager.peer(peerID)
		if peer == nil {
			player.Close()
			return
		}
		previous, accepted := peer.SetPlayer(player)
		if !accepted {
			player.Close()
			return
		}
		if previous != nil {
			previous.Close()
		}

		go func() {
			defer player.Close()
			defer peer.ClearPlayer(player)
			for {
				packet, _, err := track.ReadRTP()
				if err != nil {
					return
				}
				if err := player.WriteOpus(packet.Payload); err != nil {
					log.Printf("drop invalid Opus packet from peer %s", peerID)
				}
			}
		}()
	})

	peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		candidateJSON := candidate.ToJSON()
		payload, err := protocol.MarshalPayload(protocol.ICECandidatePayload{
			Candidate:        candidateJSON.Candidate,
			SDPMid:           candidateJSON.SDPMid,
			SDPMLineIndex:    candidateJSON.SDPMLineIndex,
			UsernameFragment: candidateJSON.UsernameFragment,
		})
		if err != nil {
			manager.notifyError(err)
			return
		}
		message := protocol.NewMessage(protocol.MsgICECandidate)
		message.PeerID = peerID
		message.Payload = payload
		if err := manager.signaler.Send(message); err != nil {
			manager.notifyError(fmt.Errorf("send ICE candidate to peer %s: %w", peerID, err))
		}
	})

	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			manager.handlePeerLeft(peerID)
		case webrtc.PeerConnectionStateDisconnected:
			go manager.removeIfStillDisconnected(peerID, peerConnection)
		}
	})

	return peerConnection, audioSender, nil
}

func (manager *MeshManager) removeIfStillDisconnected(peerID string, connection *webrtc.PeerConnection) {
	timer := time.NewTimer(disconnectGracePeriod)
	defer timer.Stop()
	select {
	case <-manager.ctx.Done():
		return
	case <-timer.C:
	}
	state := connection.ConnectionState()
	if state == webrtc.PeerConnectionStateDisconnected || state == webrtc.PeerConnectionStateFailed {
		manager.handlePeerLeft(peerID)
	}
}

func (manager *MeshManager) handlePeerJoined(message protocol.SignalingMessage) {
	if manager.ctx.Err() != nil {
		return
	}
	peerID := message.PeerID
	if err := protocol.ValidatePeerID(peerID); err != nil {
		manager.notifyError(err)
		return
	}
	username, err := protocol.NormalizeUsername(message.Username)
	if err != nil {
		manager.notifyError(err)
		return
	}
	if manager.peer(peerID) != nil {
		return
	}

	connection, sender, err := manager.createPeerConnection(peerID)
	if err != nil {
		manager.notifyError(fmt.Errorf("create peer %s: %w", peerID, err))
		return
	}
	peer := &RemotePeer{
		PeerID:       peerID,
		Username:     username,
		PC:           connection,
		AudioSender:  sender,
		pendingICE:   []webrtc.ICECandidateInit{},
		frameLimiter: rate.NewLimiter(maxRemoteFrameRate, 2),
	}

	manager.mu.Lock()
	if _, exists := manager.Peers[peerID]; exists || manager.ctx.Err() != nil {
		manager.mu.Unlock()
		peer.Close()
		return
	}
	manager.Peers[peerID] = peer
	manager.mu.Unlock()

	if manager.OnPeerJoin != nil {
		manager.OnPeerJoin(peerID, username)
	}

	if manager.LocalPeerID > peerID {
		connection.OnDataChannel(func(channel *webrtc.DataChannel) {
			manager.registerDataChannel(peer, channel)
		})
		return
	}

	maxRetransmits := uint16(0)
	ordered := false
	channel, err := connection.CreateDataChannel("video-ascii", &webrtc.DataChannelInit{
		MaxRetransmits: &maxRetransmits,
		Ordered:        &ordered,
	})
	if err != nil {
		manager.notifyError(fmt.Errorf("create video channel for peer %s: %w", peerID, err))
		manager.handlePeerLeft(peerID)
		return
	}
	manager.registerDataChannel(peer, channel)

	offer, err := connection.CreateOffer(nil)
	if err != nil {
		manager.notifyError(fmt.Errorf("create offer for peer %s: %w", peerID, err))
		return
	}
	if err := connection.SetLocalDescription(offer); err != nil {
		manager.notifyError(fmt.Errorf("set local offer for peer %s: %w", peerID, err))
		return
	}
	payload, err := protocol.MarshalPayload(protocol.SDPPayload{
		Type: "offer",
		SDP:  offer.SDP,
	})
	if err != nil {
		manager.notifyError(err)
		return
	}
	message = protocol.NewMessage(protocol.MsgOffer)
	message.PeerID = peerID
	message.Payload = payload
	if err := manager.signaler.Send(message); err != nil {
		manager.notifyError(fmt.Errorf("send offer to peer %s: %w", peerID, err))
	}
}

func (manager *MeshManager) registerDataChannel(peer *RemotePeer, channel *webrtc.DataChannel) {
	if channel.Label() != "video-ascii" {
		_ = channel.Close()
		return
	}
	if !peer.SetDataChannel(channel) {
		_ = channel.Close()
		return
	}
	channel.OnMessage(func(message webrtc.DataChannelMessage) {
		if !message.IsString || !peer.AllowFrame() {
			return
		}
		sequence, frame, ok := parseTaggedFrame(message.Data)
		if !ok || !peer.CompareAndUpdateSeq(sequence) {
			return
		}
		peer.UpdateFrame(frame)
		if manager.OnFrame != nil {
			manager.OnFrame(peer.PeerID, frame)
		}
	})
}

func (manager *MeshManager) handleOffer(message protocol.SignalingMessage) {
	peer := manager.peer(message.PeerID)
	if peer == nil {
		manager.notifyError(fmt.Errorf("offer from unknown peer %s", message.PeerID))
		return
	}
	payload, err := protocol.DecodePayload[protocol.SDPPayload](message.Payload)
	if err != nil || payload.Type != "offer" || payload.SDP == "" {
		manager.notifyError(fmt.Errorf("invalid offer from peer %s", message.PeerID))
		return
	}
	if err := peer.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  payload.SDP,
	}); err != nil {
		manager.notifyError(fmt.Errorf("set remote offer from peer %s: %w", message.PeerID, err))
		return
	}

	answer, err := peer.PC.CreateAnswer(nil)
	if err != nil {
		manager.notifyError(fmt.Errorf("create answer for peer %s: %w", message.PeerID, err))
		return
	}
	if err := peer.PC.SetLocalDescription(answer); err != nil {
		manager.notifyError(fmt.Errorf("set local answer for peer %s: %w", message.PeerID, err))
		return
	}
	answerPayload, err := protocol.MarshalPayload(protocol.SDPPayload{
		Type: "answer",
		SDP:  answer.SDP,
	})
	if err != nil {
		manager.notifyError(err)
		return
	}
	response := protocol.NewMessage(protocol.MsgAnswer)
	response.PeerID = message.PeerID
	response.Payload = answerPayload
	if err := manager.signaler.Send(response); err != nil {
		manager.notifyError(fmt.Errorf("send answer to peer %s: %w", message.PeerID, err))
	}
}

func (manager *MeshManager) handleAnswer(message protocol.SignalingMessage) {
	peer := manager.peer(message.PeerID)
	if peer == nil {
		manager.notifyError(fmt.Errorf("answer from unknown peer %s", message.PeerID))
		return
	}
	payload, err := protocol.DecodePayload[protocol.SDPPayload](message.Payload)
	if err != nil || payload.Type != "answer" || payload.SDP == "" {
		manager.notifyError(fmt.Errorf("invalid answer from peer %s", message.PeerID))
		return
	}
	if err := peer.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  payload.SDP,
	}); err != nil {
		manager.notifyError(fmt.Errorf("set remote answer from peer %s: %w", message.PeerID, err))
	}
}

func (manager *MeshManager) handleICECandidate(message protocol.SignalingMessage) {
	peer := manager.peer(message.PeerID)
	if peer == nil {
		manager.notifyError(fmt.Errorf("ICE candidate from unknown peer %s", message.PeerID))
		return
	}
	payload, err := protocol.DecodePayload[protocol.ICECandidatePayload](message.Payload)
	if err != nil || payload.Candidate == "" {
		manager.notifyError(fmt.Errorf("invalid ICE candidate from peer %s", message.PeerID))
		return
	}
	if err := peer.AddICECandidate(webrtc.ICECandidateInit{
		Candidate:        payload.Candidate,
		SDPMid:           payload.SDPMid,
		SDPMLineIndex:    payload.SDPMLineIndex,
		UsernameFragment: payload.UsernameFragment,
	}); err != nil {
		manager.notifyError(fmt.Errorf("add ICE candidate from peer %s: %w", message.PeerID, err))
	}
}

func (manager *MeshManager) handlePeerLeft(peerID string) {
	manager.mu.Lock()
	peer, exists := manager.Peers[peerID]
	if exists {
		delete(manager.Peers, peerID)
	}
	manager.mu.Unlock()
	if !exists {
		return
	}
	peer.Close()
	if manager.OnPeerLeave != nil {
		manager.OnPeerLeave(peerID)
	}
}

func (manager *MeshManager) peer(peerID string) *RemotePeer {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.Peers[peerID]
}

func (manager *MeshManager) notifyError(err error) {
	if err != nil && manager.OnError != nil {
		manager.OnError(err)
	}
}
