//go:build cgo
// +build cgo

package playback

import (
	"fmt"
	"math"
	"sync"

	"github.com/gen2brain/malgo"
	"github.com/pion/opus"
)

const (
	sampleRate = 48000
	channels   = 1 // WebRTC Opus defaults to mono/48kHz
)

type Player struct {
	dec      opus.Decoder
	device   *malgo.Device
	ctx      *malgo.AllocatedContext
	pcmChan  chan []int16
	leftover []int16
	mu       sync.Mutex
	closed   bool
	lastRMS  float64
}

// NewPlayer initializes an Opus decoder and an audio playback device.
func NewPlayer() (*Player, error) {
	dec, err := opus.NewDecoderWithOutput(sampleRate, channels)
	if err != nil {
		return nil, fmt.Errorf("failed to create opus decoder: %w", err)
	}

	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(msg string) {})
	if err != nil {
		return nil, fmt.Errorf("failed to init malgo context: %w", err)
	}

	p := &Player{
		dec:     dec,
		ctx:     ctx,
		pcmChan: make(chan []int16, 500), // Buffer ~500 frames to prevent stuttering
	}

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format = malgo.FormatS16
	deviceConfig.Playback.Channels = channels
	deviceConfig.SampleRate = sampleRate
	deviceConfig.Alsa.NoMMap = 1

	callbacks := malgo.DeviceCallbacks{
		Data: p.onAudioRequest,
	}

	device, err := malgo.InitDevice(ctx.Context, deviceConfig, callbacks)
	if err != nil {
		ctx.Free()
		return nil, fmt.Errorf("failed to init malgo device: %w", err)
	}
	p.device = device

	if err := p.device.Start(); err != nil {
		p.device.Uninit()
		ctx.Free()
		return nil, fmt.Errorf("failed to start malgo device: %w", err)
	}

	return p, nil
}

// WriteOpus decodes an Opus payload and queues it for playback.
func (p *Player) WriteOpus(data []byte) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()

	pcm := make([]int16, 2880) // 60ms * 48kHz max opus frame
	n, err := p.dec.DecodeToInt16(data, pcm)
	if err != nil {
		return err
	}

	var sum float64
	for i := 0; i < n; i++ {
		s := float64(pcm[i])
		sum += s * s
	}
	rms := 0.0
	if n > 0 {
		rms = math.Sqrt(sum / float64(n))
	}
	p.mu.Lock()
	p.lastRMS = rms / 32768.0 // Normalize to 0-1
	p.mu.Unlock()

	select {
	case p.pcmChan <- pcm[:n]:
	default:
		// Drop packet if the buffer is full to prevent falling behind live audio
	}
	return nil
}

func (p *Player) LastRMS() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastRMS
}

// onAudioRequest is called by the OS audio thread to fill the playback buffer.
func (p *Player) onAudioRequest(pOutputSample, pInputSamples []byte, framecount uint32) {
	samplesNeeded := int(framecount * channels)
	outIdx := 0

	for outIdx < samplesNeeded {
		if len(p.leftover) == 0 {
			select {
			case pcm := <-p.pcmChan:
				p.leftover = pcm
			default:
				// Buffer underrun: fill the rest of the audio frame with silence (0s)
				for i := outIdx * 2; i < len(pOutputSample); i++ {
					pOutputSample[i] = 0
				}
				return
			}
		}

		take := samplesNeeded - outIdx
		if take > len(p.leftover) {
			take = len(p.leftover)
		}

		// FormatS16 expects 2 bytes per sample (Little Endian)
		for i := 0; i < take; i++ {
			sample := p.leftover[i]
			byteIdx := (outIdx + i) * 2
			pOutputSample[byteIdx] = byte(sample)
			pOutputSample[byteIdx+1] = byte(sample >> 8)
		}

		p.leftover = p.leftover[take:]
		outIdx += take
	}
}

// Close gracefully stops the audio device and cleans up resources.
func (p *Player) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true

	if p.device != nil {
		p.device.Uninit()
	}
	if p.ctx != nil {
		p.ctx.Free()
	}
}
