package tui

import (
	"context"
	"image"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/msk1039/termcall/internal/ascii"
	"github.com/msk1039/termcall/internal/rtc"
	"github.com/pion/webrtc/v4"
)

func TestCallStartsWithoutAccessingCameraOrMicrophone(t *testing.T) {
	mesh := &fakeCallMesh{}
	camera := &fakeCamera{frames: make(chan image.Image)}
	microphone := &fakeMicrophone{}
	model := NewCallModel(context.Background(), "room", mesh, camera, microphone)

	initCommand := model.Init()
	if mesh.starts != 0 {
		t.Fatalf("mesh started synchronously during Init: starts = %d", mesh.starts)
	}
	if initCommand == nil {
		t.Fatal("Init returned no command to start the mesh")
	}
	initMessage := initCommand()
	batch, ok := initMessage.(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("Init command = %T, want a non-empty tea.BatchMsg", initMessage)
	}
	_ = batch[0]()
	if mesh.starts != 1 {
		t.Fatalf("mesh starts after running start command = %d", mesh.starts)
	}
	if camera.starts != 0 || microphone.starts != 0 || model.camOn || model.micOn {
		t.Fatalf("media accessed before consent: camera=%d microphone=%d", camera.starts, microphone.starts)
	}
}

func TestCallMediaTogglesAcquireAndReleaseDevices(t *testing.T) {
	mesh := &fakeCallMesh{}
	camera := &fakeCamera{frames: make(chan image.Image)}
	microphone := &fakeMicrophone{}
	model := NewCallModel(context.Background(), "room", mesh, camera, microphone)

	_, cameraCommand := model.Update(keyMsg("v"))
	if cameraCommand == nil || !model.camStarting {
		t.Fatal("camera did not enter the starting state")
	}
	cameraMessage := cameraCommand()
	_, _ = model.Update(cameraMessage)
	if camera.starts != 1 || !model.camOn {
		t.Fatalf("camera starts = %d, on = %v", camera.starts, model.camOn)
	}
	_, _ = model.Update(keyMsg("v"))
	if camera.closes != 1 || model.camOn {
		t.Fatalf("camera closes = %d, on = %v", camera.closes, model.camOn)
	}
	if len(mesh.frames) != 1 {
		t.Fatalf("camera-off broadcasts = %d", len(mesh.frames))
	}
	if _, err := ascii.NewDeltaDecoder().Decode(string(mesh.frames[0])); err != nil {
		t.Fatalf("camera-off frame is invalid: %v", err)
	}

	_, microphoneCommand := model.Update(keyMsg("m"))
	if microphoneCommand == nil || !model.micStarting {
		t.Fatal("microphone did not enter the starting state")
	}
	microphoneMessage := microphoneCommand()
	_, _ = model.Update(microphoneMessage)
	if microphone.starts != 1 || !model.micOn || len(mesh.muted) == 0 || mesh.muted[len(mesh.muted)-1] {
		t.Fatalf("microphone state: starts=%d on=%v muted=%v", microphone.starts, model.micOn, mesh.muted)
	}
	_, _ = model.Update(keyMsg("m"))
	if microphone.closes != 1 || model.micOn || !mesh.muted[len(mesh.muted)-1] {
		t.Fatalf("microphone state after off: closes=%d on=%v muted=%v", microphone.closes, model.micOn, mesh.muted)
	}
}

type fakeCallMesh struct {
	mu     sync.Mutex
	starts int
	muted  []bool
	frames [][]byte
}

func (mesh *fakeCallMesh) Start() {
	mesh.starts++
}

func (mesh *fakeCallMesh) SetLocalAudioTrack(webrtc.TrackLocal) {}

func (mesh *fakeCallMesh) SetMuteAudio(muted bool) {
	mesh.mu.Lock()
	defer mesh.mu.Unlock()
	mesh.muted = append(mesh.muted, muted)
}

func (mesh *fakeCallMesh) BroadcastFrame(frame []byte) {
	mesh.mu.Lock()
	defer mesh.mu.Unlock()
	mesh.frames = append(mesh.frames, append([]byte{}, frame...))
}

func (mesh *fakeCallMesh) RLockPeers(callback func(map[string]*rtc.RemotePeer)) {
	callback(map[string]*rtc.RemotePeer{})
}

type fakeCamera struct {
	frames <-chan image.Image
	starts int
	closes int
}

func (camera *fakeCamera) Start(context.Context) (<-chan image.Image, error) {
	camera.starts++
	return camera.frames, nil
}

func (camera *fakeCamera) Close() {
	camera.closes++
}

type fakeMicrophone struct {
	starts int
	closes int
}

func (microphone *fakeMicrophone) Start(context.Context) (webrtc.TrackLocal, error) {
	microphone.starts++
	return nil, nil
}

func (microphone *fakeMicrophone) LastRMS() float64 {
	return 0
}

func (microphone *fakeMicrophone) Close() {
	microphone.closes++
}
