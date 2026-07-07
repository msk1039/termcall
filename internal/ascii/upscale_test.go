package ascii

import (
	"reflect"
	"testing"
)

func TestUpscaleCells(t *testing.T) {
	// Create a 2x2 grid
	// 1 2
	// 3 4
	input := [][]Cell{
		{{Char: '1'}, {Char: '2'}},
		{{Char: '3'}, {Char: '4'}},
	}

	tests := []struct {
		name    string
		targetW int
		targetH int
		want    [][]Cell
	}{
		{
			name:    "Upscale to 4x4",
			targetW: 4,
			targetH: 4,
			want: [][]Cell{
				{{Char: '1'}, {Char: '1'}, {Char: '2'}, {Char: '2'}},
				{{Char: '1'}, {Char: '1'}, {Char: '2'}, {Char: '2'}},
				{{Char: '3'}, {Char: '3'}, {Char: '4'}, {Char: '4'}},
				{{Char: '3'}, {Char: '3'}, {Char: '4'}, {Char: '4'}},
			},
		},
		{
			name:    "Downscale to 1x1",
			targetW: 1,
			targetH: 1,
			want: [][]Cell{
				{{Char: '1'}},
			},
		},
		{
			name:    "Same size",
			targetW: 2,
			targetH: 2,
			want:    input,
		},
		{
			name:    "Non-integer scale 3x3",
			targetW: 3,
			targetH: 3,
			want: [][]Cell{
				// Indices for W=3, srcW=2: 
				// x=0: 0*2/3=0
				// x=1: 1*2/3=0
				// x=2: 2*2/3=1
				{{Char: '1'}, {Char: '1'}, {Char: '2'}},
				{{Char: '1'}, {Char: '1'}, {Char: '2'}},
				{{Char: '3'}, {Char: '3'}, {Char: '4'}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UpscaleCells(input, tt.targetW, tt.targetH)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("UpscaleCells() = \n%v\nwant \n%v", got, tt.want)
			}
		})
	}
}
