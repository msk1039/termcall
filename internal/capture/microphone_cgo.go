//go:build cgo
// +build cgo

package capture

import (
	"context"
	"fmt"

	"github.com/pion/mediadevices"
	"github.com/pion/mediadevices/pkg/codec/opus"
	_ "github.com/pion/mediadevices/pkg/driver/microphone"
	"github.com/pion/webrtc/v4"
)

type Microphone struct {
	track mediadevices.Track
}

func NewMicrophone() *Microphone {
	return &Microphone{}
}

func (m *Microphone) Start(ctx context.Context) (webrtc.TrackLocal, error) {
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
		return nil, fmt.Errorf("no audio tracks available")
	}

	m.track = tracks[0]

	// Add an audio interceptor to capture raw samples
	// Wait, we need to read raw audio data from the track's reader.
	// Since we are returning the track directly, mediadevices handles the stream.
	// How to intercept the audio track to calculate RMS?
	// The track is `mediadevices.Track`. We can wrap it or access its reader.
	// Since the track is returned and wrapped into a TrackLocal, getting raw samples here without consuming them is tricky.
	// Wait, instead of calculating here, perhaps we can use a custom interceptor, or an easier way:
	// Let's implement an atomic float64 `lastRMS` but it needs audio frames.
	// For now, let's keep it simple: we can start a goroutine that reads from the audio track.
	// BUT, reading from the track consumes it. WebRTC `AddTrack` will want to read it.
	// We can't double read. We'd have to use a broadcater.
	// Actually, the mediadevices library supports interceptors.
	// Let's just mock local volume or return 0 for now if intercepting is complex,
	// or find a way to get the local volume.

	return m.track.(webrtc.TrackLocal), nil
}

func (m *Microphone) LastRMS() float64 {
	return 0.5 // mock for now, until we figure out audio interception
}

func (m *Microphone) Close() {
	if m.track != nil {
		m.track.Close()
	}
}
