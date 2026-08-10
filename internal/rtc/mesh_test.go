package rtc

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/msk1039/termcall/internal/ascii"
	"github.com/msk1039/termcall/internal/protocol"
	"github.com/pion/webrtc/v4"
)

func TestParseTaggedFrame(t *testing.T) {
	maximumFrame := strings.Repeat("x", ascii.MaxEncodedFrameBytes)
	tests := []struct {
		name         string
		input        string
		wantSequence uint64
		wantFrame    string
		wantOK       bool
	}{
		{name: "valid", input: "42\nK 1 1 0\n-1,-1,65\n", wantSequence: 42, wantFrame: "K 1 1 0\n-1,-1,65\n", wantOK: true},
		{name: "maximum", input: "1\n" + maximumFrame, wantSequence: 1, wantFrame: maximumFrame, wantOK: true},
		{name: "no separator", input: "1", wantOK: false},
		{name: "zero sequence", input: "0\nframe", wantOK: false},
		{name: "negative sequence", input: "-1\nframe", wantOK: false},
		{name: "long sequence", input: "123456789012345678901\nframe", wantOK: false},
		{name: "empty frame", input: "1\n", wantOK: false},
		{name: "oversized frame", input: "1\n" + maximumFrame + "x", wantOK: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sequence, frame, ok := parseTaggedFrame([]byte(test.input))
			if sequence != test.wantSequence || frame != test.wantFrame || ok != test.wantOK {
				t.Fatalf("got (%d, %q, %v), want (%d, %q, %v)", sequence, frame, ok, test.wantSequence, test.wantFrame, test.wantOK)
			}
		})
	}
	if !remoteCandidateAllowedWithPrivate(net.ParseIP("10.0.0.1"), true) {
		t.Fatal("private candidate was not accepted after explicit opt-in")
	}
	if remoteCandidateAllowedWithPrivate(net.ParseIP("127.0.0.1"), true) {
		t.Fatal("loopback candidate was accepted after private-network opt-in")
	}
}

func TestRemotePeerSequenceAndPendingCandidateBounds(t *testing.T) {
	peer := &RemotePeer{pendingICE: []webrtc.ICECandidateInit{}}
	if !peer.CompareAndUpdateSeq(10) || peer.CompareAndUpdateSeq(10) || peer.CompareAndUpdateSeq(9) {
		t.Fatal("sequence replay protection failed")
	}

	for index := 0; index < maxPendingICECandidates; index++ {
		if err := peer.AddICECandidate(webrtc.ICECandidateInit{Candidate: "candidate"}); err != nil {
			t.Fatalf("candidate %d: %v", index, err)
		}
	}
	if err := peer.AddICECandidate(webrtc.ICECandidateInit{Candidate: "overflow"}); err == nil {
		t.Fatal("pending candidate limit was not enforced")
	}
	peer.Close()
	if err := peer.AddICECandidate(webrtc.ICECandidateInit{Candidate: "closed"}); !errors.Is(err, ErrPeerClosed) {
		t.Fatalf("closed peer candidate error = %v", err)
	}
}

func TestRemotePeerFrameRateIsBounded(t *testing.T) {
	peer := &RemotePeer{}
	if !peer.AllowFrame() {
		t.Fatal("first frame was unexpectedly rejected")
	}
	if !peer.AllowFrame() {
		t.Fatal("initial frame burst was unexpectedly rejected")
	}
	if peer.AllowFrame() {
		t.Fatal("frame burst exceeded the per-peer limit")
	}
	peer.Close()
	if peer.AllowFrame() {
		t.Fatal("closed peer accepted a frame")
	}
}

func TestRemotePeerSequenceIsAtomic(t *testing.T) {
	peer := &RemotePeer{}
	var accepted atomic.Int64
	var waitGroup sync.WaitGroup
	for sequence := uint64(1); sequence <= 100; sequence++ {
		sequence := sequence
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if peer.CompareAndUpdateSeq(sequence) {
				accepted.Add(1)
			}
		}()
	}
	waitGroup.Wait()
	if peer.lastSeq.Load() != 100 {
		t.Fatalf("last sequence = %d", peer.lastSeq.Load())
	}
	if accepted.Load() < 1 || accepted.Load() > 100 {
		t.Fatalf("accepted sequence count = %d", accepted.Load())
	}
}

func TestCounterDeltaHandlesCounterReset(t *testing.T) {
	if delta := counterDelta(150, 100); delta != 50 {
		t.Fatalf("normal delta = %d", delta)
	}
	if delta := counterDelta(10, 100); delta != 10 {
		t.Fatalf("reset delta = %d", delta)
	}
}

func TestRemoteCandidatePolicyBlocksLocalNetworkTargets(t *testing.T) {
	tests := []struct {
		ip      string
		allowed bool
	}{
		{ip: "127.0.0.1", allowed: false},
		{ip: "10.0.0.1", allowed: false},
		{ip: "169.254.1.1", allowed: false},
		{ip: "100.64.0.1", allowed: false},
		{ip: "192.0.2.1", allowed: false},
		{ip: "2001:db8::1", allowed: false},
		{ip: "1.1.1.1", allowed: true},
		{ip: "2606:4700:4700::1111", allowed: true},
	}
	for _, test := range tests {
		if got := remoteCandidateAllowed(net.ParseIP(test.ip)); got != test.allowed {
			t.Errorf("remoteCandidateAllowed(%s) = %v", test.ip, got)
		}
	}
}

func TestMeshLifecycleUsesParentContextAndStopsOnce(t *testing.T) {
	transport := newFakeSignalTransport()
	manager, err := NewMeshManager(context.Background(), transport, protocol.JoinAcceptedPayload{
		PeerID: "local-peer",
	})
	if err != nil {
		t.Fatalf("new mesh: %v", err)
	}
	reported := make(chan error, 1)
	manager.OnError = func(err error) {
		select {
		case reported <- err:
		default:
		}
	}
	manager.Start()
	manager.Start()
	wantError := errors.New("transport failed")
	transport.errs <- wantError
	select {
	case got := <-reported:
		if !errors.Is(got, wantError) {
			t.Fatalf("reported error = %v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("mesh did not report the transport error")
	}

	manager.Stop()
	manager.Stop()
	if transport.closeCount.Load() != 1 {
		t.Fatalf("transport close count = %d", transport.closeCount.Load())
	}
}

func TestMutedMeshCanAnswerInitialAudioOffer(t *testing.T) {
	transport := newFakeSignalTransport()
	manager, err := NewMeshManager(context.Background(), transport, protocol.JoinAcceptedPayload{
		PeerID: "answering-peer",
	})
	if err != nil {
		t.Fatalf("new mesh: %v", err)
	}
	defer manager.Stop()

	offerer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("new offering peer connection: %v", err)
	}
	defer offerer.Close()
	offerTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeOpus,
			ClockRate: 48000,
			Channels:  2,
		},
		"audio",
		"test",
	)
	if err != nil {
		t.Fatalf("new offer track: %v", err)
	}
	if _, err := offerer.AddTrack(offerTrack); err != nil {
		t.Fatalf("add offer track: %v", err)
	}
	offer, err := offerer.CreateOffer(nil)
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}
	if err := offerer.SetLocalDescription(offer); err != nil {
		t.Fatalf("set local offer: %v", err)
	}

	answerer, _, err := manager.createPeerConnection("offering-peer")
	if err != nil {
		t.Fatalf("create answering peer connection: %v", err)
	}
	defer answerer.Close()
	if err := answerer.SetRemoteDescription(offer); err != nil {
		t.Fatalf("set remote offer: %v", err)
	}
	answer, err := answerer.CreateAnswer(nil)
	if err != nil {
		t.Fatalf("create answer: %v", err)
	}
	if err := answerer.SetLocalDescription(answer); err != nil {
		t.Fatalf("set local answer while muted: %v", err)
	}
}

func TestNewMeshManagerValidatesInputs(t *testing.T) {
	transport := newFakeSignalTransport()
	if _, err := NewMeshManager(context.Background(), nil, protocol.JoinAcceptedPayload{PeerID: "peer"}); err == nil {
		t.Fatal("nil transport unexpectedly succeeded")
	}
	if _, err := NewMeshManager(context.Background(), transport, protocol.JoinAcceptedPayload{PeerID: "bad/peer"}); !errors.Is(err, protocol.ErrInvalidPeerID) {
		t.Fatalf("invalid peer error = %v", err)
	}
}

type fakeSignalTransport struct {
	messages   chan protocol.SignalingMessage
	errs       chan error
	closeOnce  sync.Once
	closeCount atomic.Int64
}

func newFakeSignalTransport() *fakeSignalTransport {
	return &fakeSignalTransport{
		messages: make(chan protocol.SignalingMessage),
		errs:     make(chan error, 1),
	}
}

func (transport *fakeSignalTransport) Send(protocol.SignalingMessage) error {
	return nil
}

func (transport *fakeSignalTransport) Receive() <-chan protocol.SignalingMessage {
	return transport.messages
}

func (transport *fakeSignalTransport) Errors() <-chan error {
	return transport.errs
}

func (transport *fakeSignalTransport) Close() error {
	transport.closeOnce.Do(func() {
		transport.closeCount.Add(1)
		close(transport.messages)
		close(transport.errs)
	})
	return nil
}
