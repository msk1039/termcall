//go:build !cgo

package capture

import (
	"context"
	"errors"
	"image"
)

// Camera is a receive-only stub for builds without native media support.
type Camera struct{}

func NewCamera(_ int) *Camera {
	return &Camera{}
}

func (camera *Camera) Start(_ context.Context) (<-chan image.Image, error) {
	return nil, errors.New("camera capture is disabled because TermCall was built without CGO")
}

func (camera *Camera) Close() {}
