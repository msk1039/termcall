package ascii

import (
	"errors"
	"strings"
	"testing"
)

func TestDeltaDecoderRejectsInvalidFrames(t *testing.T) {
	tests := []struct {
		name    string
		frame   string
		wantErr error
	}{
		{name: "empty", frame: "", wantErr: ErrMalformedFrame},
		{name: "raw ansi", frame: "\x1b[31mred", wantErr: ErrUnsupportedFrame},
		{name: "huge dimensions", frame: "K 1000000000 1 0\n", wantErr: ErrFrameTooLarge},
		{name: "wide dimensions", frame: "K 101 1 0\n", wantErr: ErrFrameTooLarge},
		{name: "invalid mode", frame: "K 1 1 9\n-1,-1,65\n", wantErr: ErrInvalidFrameValue},
		{name: "missing row", frame: "K 1 2 0\n-1,-1,65\n", wantErr: ErrMalformedFrame},
		{name: "extra cell", frame: "K 1 1 0\n-1,-1,65;-1,-1,66\n", wantErr: ErrMalformedFrame},
		{name: "invalid color", frame: "K 1 1 0\n256,-1,65\n", wantErr: ErrInvalidFrameValue},
		{name: "terminal escape character", frame: "K 1 1 0\n-1,-1,27\n", wantErr: ErrInvalidFrameValue},
		{name: "nul outside half block", frame: "K 1 1 0\n-1,-1,0\n", wantErr: ErrInvalidFrameValue},
		{name: "delta before keyframe", frame: "D 1 1 0\n", wantErr: ErrMissingKeyframe},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decoder := NewDeltaDecoder()
			_, err := decoder.Decode(test.frame)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestDeltaDecoderRejectsOversizedPayloadBeforeParsing(t *testing.T) {
	decoder := NewDeltaDecoder()
	payload := "K" + strings.Repeat("0", MaxEncodedFrameBytes)
	if _, err := decoder.Decode(payload); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("error = %v", err)
	}
}

func TestInvalidDeltaDoesNotMutateDecoderState(t *testing.T) {
	decoder := NewDeltaDecoder()
	seed := "K 2 1 0\n-1,-1,65;-1,-1,66\n"
	if _, err := decoder.Decode(seed); err != nil {
		t.Fatalf("seed decoder: %v", err)
	}
	invalid := "D 2 1 2\n0 0 -1 -1 88\n0 0 -1 -1 89\n"
	if _, err := decoder.Decode(invalid); !errors.Is(err, ErrMalformedFrame) {
		t.Fatalf("invalid delta error = %v", err)
	}
	cells, mode, err := decoder.DecodeCells("D 2 1 0\n")
	if err != nil {
		t.Fatalf("decode after invalid delta: %v", err)
	}
	if mode != ModeASCII || string([]byte{cells[0][0].Char, cells[0][1].Char}) != "AB" {
		t.Fatalf("decoder state mutated: %#v", cells)
	}

	cells[0][0].Char = 'Z'
	cellsAgain, _, err := decoder.DecodeCells("D 2 1 0\n")
	if err != nil {
		t.Fatalf("decode after caller mutation: %v", err)
	}
	if cellsAgain[0][0].Char != 'A' {
		t.Fatal("DecodeCells exposed the decoder's internal grid")
	}
}

func TestDeltaDecoderRejectsDuplicateAndOutOfBoundsChanges(t *testing.T) {
	tests := []struct {
		name    string
		delta   string
		wantErr error
	}{
		{name: "duplicate", delta: "D 2 1 2\n0 1 -1 -1 67\n0 1 -1 -1 68\n", wantErr: ErrMalformedFrame},
		{name: "out of bounds", delta: "D 2 1 1\n0 2 -1 -1 67\n", wantErr: ErrInvalidFrameValue},
		{name: "wrong count", delta: "D 2 1 2\n0 1 -1 -1 67\n", wantErr: ErrMalformedFrame},
		{name: "count over cells", delta: "D 2 1 3\n", wantErr: ErrInvalidFrameValue},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decoder := NewDeltaDecoder()
			if _, err := decoder.Decode("K 2 1 0\n-1,-1,65;-1,-1,66\n"); err != nil {
				t.Fatalf("seed decoder: %v", err)
			}
			if _, err := decoder.Decode(test.delta); !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestStatusFrameIsBoundedAndSafe(t *testing.T) {
	frame := StatusFrame("off\x1b[31m\n" + strings.Repeat("x", 100))
	if len(frame) > MaxEncodedFrameBytes {
		t.Fatalf("status frame length = %d", len(frame))
	}
	decoded, err := NewDeltaDecoder().Decode(frame)
	if err != nil {
		t.Fatalf("decode status frame: %v", err)
	}
	if strings.ContainsRune(decoded, '\x1b') {
		t.Fatal("status frame contains a terminal escape")
	}
}

func FuzzDeltaDecoder(f *testing.F) {
	f.Add("K 1 1 0\n-1,-1,65\n")
	f.Add("D 1 1 1\n0 0 -1 -1 66\n")
	f.Add("\x1b[2J")
	f.Fuzz(func(t *testing.T, frame string) {
		if len(frame) > MaxEncodedFrameBytes*2 {
			t.Skip()
		}
		decoder := NewDeltaDecoder()
		_, _ = decoder.Decode(frame)
	})
}
