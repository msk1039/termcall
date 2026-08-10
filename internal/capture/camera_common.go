package capture

import (
	"context"
	"image"
)

// FrameSource is the camera boundary used by the terminal UI.
type FrameSource interface {
	Start(ctx context.Context) (<-chan image.Image, error)
	Close()
}
