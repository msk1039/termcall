//go:build !cgo
// +build !cgo

package capture

import (
	"context"
	"fmt"

	"github.com/pion/webrtc/v4"
)

type Microphone struct {
}

func NewMicrophone() *Microphone {
	return &Microphone{}
}

func (m *Microphone) Start(ctx context.Context) (webrtc.TrackLocal, error) {
	return nil, fmt.Errorf("audio capture is disabled because the application was built without CGO (e.g. cross-compiled for Windows)")
}

func (m *Microphone) LastRMS() float64 {
	return 0.0
}

func (m *Microphone) Close() {
	// No-op
}
