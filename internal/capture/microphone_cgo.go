//go:build cgo
// +build cgo

package capture

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/pion/mediadevices"
	"github.com/pion/mediadevices/pkg/codec/opus"
	_ "github.com/pion/mediadevices/pkg/driver/microphone"
	"github.com/pion/webrtc/v4"
)

type Microphone struct {
	track      mediadevices.Track
	mu         sync.Mutex
	starting   bool
	generation uint64
}

func NewMicrophone() *Microphone {
	return &Microphone{}
}

func (m *Microphone) Start(ctx context.Context) (webrtc.TrackLocal, error) {
	m.mu.Lock()
	if m.track != nil || m.starting {
		m.mu.Unlock()
		return nil, errors.New("microphone is already running")
	}
	m.starting = true
	generation := m.generation
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.starting = false
		m.mu.Unlock()
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	opusParams, err := opus.NewParams()
	if err != nil {
		return nil, fmt.Errorf("opus params: %w", err)
	}

	stream, err := mediadevices.GetUserMedia(mediadevices.MediaStreamConstraints{
		Audio: func(c *mediadevices.MediaTrackConstraints) {},
		Codec: mediadevices.NewCodecSelector(
			mediadevices.WithAudioEncoders(&opusParams),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("getting user media (mic): %w", err)
	}

	tracks := stream.GetAudioTracks()
	if len(tracks) == 0 {
		closeTracks(stream.GetTracks())
		return nil, fmt.Errorf("no audio tracks available")
	}

	track := tracks[0]
	localTrack, ok := track.(webrtc.TrackLocal)
	if !ok {
		closeTracks(stream.GetTracks())
		return nil, errors.New("microphone returned an unsupported audio track")
	}
	for _, extraTrack := range stream.GetTracks() {
		if extraTrack != track {
			_ = extraTrack.Close()
		}
	}

	m.mu.Lock()
	if m.generation != generation || ctx.Err() != nil {
		m.mu.Unlock()
		closeTracks(stream.GetTracks())
		return nil, context.Canceled
	}
	m.track = track
	m.mu.Unlock()
	return localTrack, nil
}

func (m *Microphone) LastRMS() float64 {
	return 0
}

func (m *Microphone) Close() {
	m.mu.Lock()
	m.generation++
	track := m.track
	m.track = nil
	m.mu.Unlock()
	if track != nil {
		_ = track.Close()
	}
}
