package capture

import (
	"context"
	"fmt"

	"image"

	"github.com/pion/mediadevices"
	_ "github.com/pion/mediadevices/pkg/driver/camera" // Register camera adapter
	"github.com/pion/mediadevices/pkg/prop"
)

// FrameSource is an interface for something that produces frames
type FrameSource interface {
	Start(ctx context.Context, width, height int) (<-chan image.Image, error)
}

type Camera struct {
	frameRate int
}

func NewCamera(fps int) *Camera {
	return &Camera{frameRate: fps}
}

// Start begins producing frames
func (c *Camera) Start(ctx context.Context) (<-chan image.Image, error) {
	stream, err := mediadevices.GetUserMedia(mediadevices.MediaStreamConstraints{
		Video: func(mtc *mediadevices.MediaTrackConstraints) {
			mtc.FrameRate = prop.Float(c.frameRate)
			mtc.Width = prop.Int(640)
			mtc.Height = prop.Int(480)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("getting user media: %w", err)
	}

	tracks := stream.GetVideoTracks()
	if len(tracks) == 0 {
		return nil, fmt.Errorf("no video tracks available")
	}

	track := tracks[0]
	videoReader := track.(*mediadevices.VideoTrack).NewReader(false)

	frameChan := make(chan image.Image)

	go func() {
		defer close(frameChan)
		defer track.Close()

		for {
			select {
			case <-ctx.Done():
				return
			default:
				img, _, err := videoReader.Read()
				if err != nil {
					fmt.Printf("Error reading video frame: %v\n", err)
					return
				}

				select {
				case frameChan <- img:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return frameChan, nil
}
