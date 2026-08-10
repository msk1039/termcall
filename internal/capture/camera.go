//go:build cgo

package capture

import (
	"context"
	"errors"
	"fmt"
	"image"
	"sync"

	"github.com/pion/mediadevices"
	_ "github.com/pion/mediadevices/pkg/driver/camera"
	"github.com/pion/mediadevices/pkg/prop"
)

type Camera struct {
	frameRate  int
	mu         sync.Mutex
	session    *cameraSession
	starting   bool
	generation uint64
}

type cameraSession struct {
	cancel context.CancelFunc
	track  mediadevices.Track
	once   sync.Once
}

func NewCamera(fps int) *Camera {
	return &Camera{frameRate: fps}
}

func (camera *Camera) Start(ctx context.Context) (<-chan image.Image, error) {
	camera.mu.Lock()
	if camera.session != nil || camera.starting {
		camera.mu.Unlock()
		return nil, errors.New("camera is already running")
	}
	camera.starting = true
	generation := camera.generation
	camera.mu.Unlock()
	defer func() {
		camera.mu.Lock()
		camera.starting = false
		camera.mu.Unlock()
	}()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stream, err := mediadevices.GetUserMedia(mediadevices.MediaStreamConstraints{
		Video: func(constraints *mediadevices.MediaTrackConstraints) {
			constraints.FrameRate = prop.Float(camera.frameRate)
			constraints.Width = prop.Int(640)
			constraints.Height = prop.Int(480)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get camera media: %w", err)
	}

	tracks := stream.GetVideoTracks()
	if len(tracks) == 0 {
		closeTracks(stream.GetTracks())
		return nil, errors.New("no video tracks available")
	}
	track := tracks[0]
	videoTrack, ok := track.(*mediadevices.VideoTrack)
	if !ok {
		closeTracks(stream.GetTracks())
		return nil, errors.New("camera returned an unsupported video track")
	}
	for _, extraTrack := range stream.GetTracks() {
		if extraTrack != track {
			_ = extraTrack.Close()
		}
	}

	sessionCtx, cancel := context.WithCancel(ctx)
	session := &cameraSession{
		cancel: cancel,
		track:  track,
	}
	camera.mu.Lock()
	if camera.generation != generation || ctx.Err() != nil {
		camera.mu.Unlock()
		session.close()
		return nil, context.Canceled
	}
	camera.session = session
	camera.mu.Unlock()

	frames := make(chan image.Image)
	reader := videoTrack.NewReader(false)
	go func() {
		defer close(frames)
		defer camera.clearSession(session)
		defer session.close()
		for {
			if sessionCtx.Err() != nil {
				return
			}
			frame, _, err := reader.Read()
			if err != nil {
				return
			}
			select {
			case frames <- frame:
			case <-sessionCtx.Done():
				return
			}
		}
	}()
	return frames, nil
}

func (camera *Camera) Close() {
	camera.mu.Lock()
	camera.generation++
	session := camera.session
	camera.session = nil
	camera.mu.Unlock()
	if session != nil {
		session.close()
	}
}

func (camera *Camera) clearSession(session *cameraSession) {
	camera.mu.Lock()
	defer camera.mu.Unlock()
	if camera.session == session {
		camera.session = nil
	}
}

func (session *cameraSession) close() {
	session.once.Do(func() {
		session.cancel()
		_ = session.track.Close()
	})
}

func closeTracks(tracks []mediadevices.Track) {
	for _, track := range tracks {
		_ = track.Close()
	}
}
