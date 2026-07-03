// Package globe provides a customizable ASCII globe generator.
// Based on C++ code by DinoZ1729 (https://github.com/DinoZ1729/Earth)
// and the Rust implementation by adamsky.
package globe

import (
	_ "embed"
	"math"
	"os"
	"strings"
)

//go:embed textures/earth.txt
var earthTexture string

//go:embed textures/earth_night.txt
var earthNightTexture string

//go:embed textures/earth_cities.txt
var earthCitiesTexture string

//go:embed textures/earth_clouds.txt
var earthCloudsTexture string

// Texture represents a globe texture with optional night, palette, cities and clouds data.
type Texture struct {
	Day     [][]rune
	Night   [][]rune
	Cities  [][]rune
	Clouds  [][]rune
	Palette []rune
}

// NewTexture creates a new Texture.
func NewTexture(day [][]rune, night [][]rune, palette []rune) *Texture {
	return &Texture{
		Day:     day,
		Night:   night,
		Palette: palette,
	}
}

// GetSize returns the usable texture size (original dimensions minus one).
func (t *Texture) GetSize() (int, int) {
	return len(t.Day[0]) - 1, len(t.Day) - 1
}

// Canvas is the rendering target for the globe.
type Canvas struct {
	Matrix  [][]rune
	Night   [][]bool // Night[i][j] == true marks cells on the unlit hemisphere.
	Cloud   [][]bool // Cloud[i][j] == true marks cells that are cloud cover.
	size    [2]int
	CharPix [2]int
}

// NewCanvas creates a new Canvas with the given dimensions and optional character pixel size.
func NewCanvas(x, y int, cp *[2]int) *Canvas {
	matrix := make([][]rune, y)
	night := make([][]bool, y)
	cloud := make([][]bool, y)
	for i := range matrix {
		matrix[i] = make([]rune, x)
		night[i] = make([]bool, x)
		cloud[i] = make([]bool, x)
		for j := range matrix[i] {
			matrix[i][j] = ' '
		}
	}
	charPix := [2]int{4, 8}
	if cp != nil {
		charPix = *cp
	}
	return &Canvas{
		Matrix:  matrix,
		Night:   night,
		Cloud:   cloud,
		size:    [2]int{x, y},
		CharPix: charPix,
	}
}

// GetSize returns the canvas dimensions.
func (c *Canvas) GetSize() (int, int) {
	return c.size[0], c.size[1]
}

// Clear resets the canvas to spaces.
func (c *Canvas) Clear() {
	for i := range c.Matrix {
		for j := range c.Matrix[i] {
			c.Matrix[i][j] = ' '
			c.Night[i][j] = false
			c.Cloud[i][j] = false
		}
	}
}

func (c *Canvas) drawPoint(a, b int, ch rune) {
	if a < 0 || b < 0 || a >= c.size[0] || b >= c.size[1] {
		return
	}
	c.Matrix[b][a] = ch
}

// drawNightPoint marks a cell as part of the unlit hemisphere and writes its
// character (a land glyph from the day texture, or a city light marker).
func (c *Canvas) drawNightPoint(a, b int, ch rune) {
	if a < 0 || b < 0 || a >= c.size[0] || b >= c.size[1] {
		return
	}
	c.Matrix[b][a] = ch
	c.Night[b][a] = true
}

// drawCloudPoint marks a cell as part of the cloud layer and writes its
// cloud glyph. Cloud cells keep whichever night/day flag they were already
// tagged with (clouds exist on both hemispheres).
func (c *Canvas) drawCloudPoint(a, b int, ch rune) {
	if a < 0 || b < 0 || a >= c.size[0] || b >= c.size[1] {
		return
	}
	c.Matrix[b][a] = ch
	c.Cloud[b][a] = true
}

// Globe is the main globe abstraction.
type Globe struct {
	Camera        *Camera
	Radius        float32
	Angle         float32
	CloudAngle    float32 // drift angle for the cloud layer (parallax source)
	Texture       *Texture
	DisplayNight  bool
	DisplayClouds bool
}

// RenderOn renders the globe onto the given canvas.
func (g *Globe) RenderOn(canvas *Canvas) {
	light := [3]float32{0, 999999, 0}
	sizeX, sizeY := canvas.GetSize()
	for yi := 0; yi < sizeY; yi++ {
		yif := float32(yi)
		for xi := 0; xi < sizeX; xi++ {
			xif := float32(xi)
			// camera position (origin of the ray)
			o := [3]float32{g.Camera.x, g.Camera.y, g.Camera.z}
			// direction of the ray
			u := [3]float32{
				-((xif - float32(sizeX/canvas.CharPix[0]/2)) + 0.5) /
					float32(sizeX/canvas.CharPix[0]/2),
				((yif - float32(sizeY/canvas.CharPix[1]/2)) + 0.5) /
					float32(sizeY/canvas.CharPix[1]/2),
				-1,
			}
			transformVector(&u, g.Camera.matrix)
			u[0] -= g.Camera.x
			u[1] -= g.Camera.y
			u[2] -= g.Camera.z
			normalize(&u)
			dotUO := dot(&u, &o)
			discriminant := dotUO*dotUO - dot(&o, &o) + g.Radius*g.Radius

			if discriminant < 0 {
				continue
			}

			distance := -float32(math.Sqrt(float64(discriminant))) - dotUO

			// intersection point
			inter := [3]float32{
				o[0] + distance*u[0],
				o[1] + distance*u[1],
				o[2] + distance*u[2],
			}

			// surface normal
			n := [3]float32{
				o[0] + distance*u[0],
				o[1] + distance*u[1],
				o[2] + distance*u[2],
			}
			normalize(&n)

			// unit vector from intersection to light source
			l := [3]float32{}
			vector(&l, &inter, &light)
			normalize(&l)
			luminance := clamp(5*dot(&n, &l)+0.5, 0, 1)
			temp := [3]float32{inter[0], inter[1], inter[2]}
			rotateX(&temp, -float32(math.Pi)*2*0/360)

			// sphere coordinates
			phi := -temp[2]/g.Radius/2 + 0.5
			theta := float32(math.Atan(float64(temp[1]/temp[0])))/float32(math.Pi) + 0.5 + g.Angle/2/float32(math.Pi)
			theta -= float32(math.Floor(float64(theta)))

			texX, texY := g.Texture.GetSize()
			earthX := int(theta * float32(texX))
			earthY := int(phi * float32(texY))

			isNight := g.DisplayNight && g.Texture.Cities != nil && luminance <= 0.5
			isDay := g.DisplayNight && g.Texture.Cities != nil && luminance > 0.5

			if isNight {
				// Night side: dim land, except where a city light marker exists.
				if cityChar := g.Texture.Cities[earthY][earthX]; cityChar != ' ' {
					canvas.drawNightPoint(xi, yi, cityChar)
				} else {
					canvas.drawNightPoint(xi, yi, g.Texture.Day[earthY][earthX])
				}
			} else if isDay {
				// Day side: normal bright day texture.
				canvas.drawPoint(xi, yi, g.Texture.Day[earthY][earthX])
			} else {
				// Night display disabled: just paint the day texture everywhere.
				canvas.drawPoint(xi, yi, g.Texture.Day[earthY][earthX])
			}

			// ── Cloud layer (optional, modular) ───────────────────────────
			// Sample the cloud texture at a separately-drifting longitude so it
			// scrolls over the landmass at a slightly different rate, creating
			// parallax. The cloud glyph replaces the land glyph but keeps the
			// night flag the cell already received above. City lights are NOT
			// overwritten — they shine through thin clouds. On the day side
			// clouds read as bright white; on the night side they read as very
			// dim gray (handled by styleForCell).
			if g.DisplayClouds && g.Texture.Clouds != nil {
				isCity := isNight && g.Texture.Cities[earthY][earthX] != ' '
				if !isCity {
					cloudTheta := theta + g.CloudAngle/2/float32(math.Pi)
					cloudTheta -= float32(math.Floor(float64(cloudTheta)))
					cloudX := int(cloudTheta * float32(texX))
					if cloudX < 0 || cloudX >= len(g.Texture.Clouds[earthY]) {
						cloudX = 0
					}
					if cloudChar := g.Texture.Clouds[earthY][cloudX]; cloudChar != ' ' {
						canvas.drawCloudPoint(xi, yi, cloudChar)
					}
				}
			}
		}
	}
}

// GlobeTemplate represents built-in globe templates.
type GlobeTemplate int

const (
	Earth GlobeTemplate = iota
)

// GlobeConfig implements the builder pattern for Globe.
type GlobeConfig struct {
	cameraCfg     *CameraConfig
	radius        *float32
	angle         *float32
	template      *GlobeTemplate
	texture       *Texture
	displayNight  bool
	displayClouds bool
}

// NewGlobeConfig creates an empty GlobeConfig.
func NewGlobeConfig() *GlobeConfig {
	return &GlobeConfig{}
}

// WithCamera sets the camera config.
func (gc *GlobeConfig) WithCamera(config *CameraConfig) *GlobeConfig {
	gc.cameraCfg = config
	return gc
}

// WithRadius sets the globe radius.
func (gc *GlobeConfig) WithRadius(r float32) *GlobeConfig {
	gc.radius = &r
	return gc
}

// UseTemplate selects a built-in template.
func (gc *GlobeConfig) UseTemplate(t GlobeTemplate) *GlobeConfig {
	gc.template = &t
	return gc
}

// WithTexture sets the day texture from a string.
func (gc *GlobeConfig) WithTexture(texture string, palette []rune) *GlobeConfig {
	day := parseTexture(texture)
	if gc.texture != nil {
		gc.texture.Day = day
	} else {
		gc.texture = NewTexture(day, nil, palette)
	}
	return gc
}

// WithNightTexture sets the night texture from a string.
func (gc *GlobeConfig) WithNightTexture(texture string, palette []rune) *GlobeConfig {
	night := parseTexture(texture)
	if gc.texture != nil {
		gc.texture.Night = night
	} else {
		gc.texture = NewTexture(night, night, palette)
	}
	return gc
}

// WithTextureAt loads a day texture from a file path.
func (gc *GlobeConfig) WithTextureAt(path string, palette []rune) *GlobeConfig {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return gc.WithTexture(string(data), palette)
}

// WithNightTextureAt loads a night texture from a file path.
func (gc *GlobeConfig) WithNightTextureAt(path string, palette []rune) *GlobeConfig {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return gc.WithNightTexture(string(data), palette)
}

// WithCitiesTexture sets the cities overlay texture from a string.
func (gc *GlobeConfig) WithCitiesTexture(texture string) *GlobeConfig {
	cities := parseTexture(texture)
	if gc.texture != nil {
		gc.texture.Cities = cities
	} else {
		gc.texture = &Texture{Cities: cities}
	}
	return gc
}

// WithCitiesTextureAt loads a cities overlay texture from a file path.
func (gc *GlobeConfig) WithCitiesTextureAt(path string) *GlobeConfig {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return gc.WithCitiesTexture(string(data))
}

// DisplayNight sets the night display toggle.
func (gc *GlobeConfig) DisplayNight(b bool) *GlobeConfig {
	gc.displayNight = b
	return gc
}

// WithCloudsTexture sets the cloud overlay texture from a string.
func (gc *GlobeConfig) WithCloudsTexture(texture string) *GlobeConfig {
	clouds := parseTexture(texture)
	if gc.texture != nil {
		gc.texture.Clouds = clouds
	} else {
		gc.texture = &Texture{Clouds: clouds}
	}
	return gc
}

// WithCloudsTextureAt loads a cloud overlay texture from a file path.
func (gc *GlobeConfig) WithCloudsTextureAt(path string) *GlobeConfig {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return gc.WithCloudsTexture(string(data))
}

// DisplayClouds toggles whether the cloud layer is rendered.
func (gc *GlobeConfig) DisplayClouds(b bool) *GlobeConfig {
	gc.displayClouds = b
	return gc
}

// Build constructs a Globe from the configuration.
func (gc *GlobeConfig) Build() *Globe {
	if gc.template != nil {
		switch *gc.template {
		case Earth:
			palette := []rune{' ', '.', ':', ';', '\'', ',', 'w', 'i', 'o', 'g', 'O', 'L', 'X', 'H', 'W', 'Y', 'V', '@'}
			gc.WithTexture(earthTexture, palette)
			gc.WithNightTexture(earthNightTexture, palette)
			gc.WithCitiesTexture(earthCitiesTexture)
			gc.WithCloudsTexture(earthCloudsTexture)
		}
	}
	if gc.texture == nil {
		panic("texture not provided")
	}
	var camera *Camera
	if gc.cameraCfg != nil {
		camera = gc.cameraCfg.Build()
	} else {
		camera = NewCameraConfig(2, 0, 0).Build()
	}
	radius := float32(1)
	if gc.radius != nil {
		radius = *gc.radius
	}
	angle := float32(0)
	if gc.angle != nil {
		angle = *gc.angle
	}
	return &Globe{
		Camera:        camera,
		Radius:        radius,
		Angle:         angle,
		Texture:       gc.texture,
		DisplayNight:  gc.displayNight,
		DisplayClouds: gc.displayClouds,
	}
}

func parseTexture(texture string) [][]rune {
	texture = strings.ReplaceAll(texture, "\r\n", "\n")
	lines := strings.Split(texture, "\n")
	// Match Rust's str::lines() behavior: omit trailing empty string
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	result := make([][]rune, 0, len(lines))
	for _, line := range lines {
		row := make([]rune, len(line))
		for i, ch := range line {
			row[len(line)-1-i] = ch
		}
		result = append(result, row)
	}
	return result
}

// CameraConfig configures the camera.
type CameraConfig struct {
	Radius float32
	Alpha  float32
	Beta   float32
}

// NewCameraConfig creates a new CameraConfig.
func NewCameraConfig(radius, alpha, beta float32) *CameraConfig {
	return &CameraConfig{Radius: radius, Alpha: alpha, Beta: beta}
}

// DefaultCameraConfig creates a CameraConfig with default values.
func DefaultCameraConfig() *CameraConfig {
	return &CameraConfig{Radius: 2, Alpha: 0, Beta: 0}
}

// Build constructs a Camera from the config.
func (cc *CameraConfig) Build() *Camera {
	camera := &Camera{}
	camera.Update(cc.Radius, cc.Alpha, cc.Beta)
	return camera
}

// Camera represents the scene camera.
type Camera struct {
	x      float32
	y      float32
	z      float32
	matrix [16]float32
	inv    [16]float32
}

// Update updates the camera using new data.
func (c *Camera) Update(r, alpha, beta float32) {
	sinA := float32(math.Sin(float64(alpha)))
	cosA := float32(math.Cos(float64(alpha)))
	sinB := float32(math.Sin(float64(beta)))
	cosB := float32(math.Cos(float64(beta)))

	x := r * cosA * cosB
	y := r * sinA * cosB
	z := r * sinB

	var matrix [16]float32
	matrix[3] = 0
	matrix[7] = 0
	matrix[11] = 0
	matrix[15] = 1
	// x
	matrix[0] = -sinA
	matrix[1] = cosA
	matrix[2] = 0
	// y
	matrix[4] = cosA * sinB
	matrix[5] = sinA * sinB
	matrix[6] = -cosB
	// z
	matrix[8] = cosA * cosB
	matrix[9] = sinA * cosB
	matrix[10] = sinB

	matrix[12] = x
	matrix[13] = y
	matrix[14] = z

	var inv [16]float32
	invert(&inv, matrix)

	c.x = x
	c.y = y
	c.z = z
	c.matrix = matrix
	c.inv = inv
}

func findIndex(target rune, palette []rune) int {
	for i, ch := range palette {
		if target == ch {
			return i
		}
	}
	return -1
}

func transformVector(vec *[3]float32, m [16]float32) {
	tx := vec[0]*m[0] + vec[1]*m[4] + vec[2]*m[8] + m[12]
	ty := vec[0]*m[1] + vec[1]*m[5] + vec[2]*m[9] + m[13]
	tz := vec[0]*m[2] + vec[1]*m[6] + vec[2]*m[10] + m[14]
	vec[0] = tx
	vec[1] = ty
	vec[2] = tz
}

func invert(inv *[16]float32, matrix [16]float32) {
	inv[0] = matrix[5]*matrix[10]*matrix[15] -
		matrix[5]*matrix[11]*matrix[14] -
		matrix[9]*matrix[6]*matrix[15] +
		matrix[9]*matrix[7]*matrix[14] +
		matrix[13]*matrix[6]*matrix[11] -
		matrix[13]*matrix[7]*matrix[10]

	inv[4] = -matrix[4]*matrix[10]*matrix[15] +
		matrix[4]*matrix[11]*matrix[14] +
		matrix[8]*matrix[6]*matrix[15] -
		matrix[8]*matrix[7]*matrix[14] -
		matrix[12]*matrix[6]*matrix[11] +
		matrix[12]*matrix[7]*matrix[10]

	inv[8] = matrix[4]*matrix[9]*matrix[15] -
		matrix[4]*matrix[11]*matrix[13] -
		matrix[8]*matrix[5]*matrix[15] +
		matrix[8]*matrix[7]*matrix[13] +
		matrix[12]*matrix[5]*matrix[11] -
		matrix[12]*matrix[7]*matrix[9]

	inv[12] = -matrix[4]*matrix[9]*matrix[14] +
		matrix[4]*matrix[10]*matrix[13] +
		matrix[8]*matrix[5]*matrix[14] -
		matrix[8]*matrix[6]*matrix[13] -
		matrix[12]*matrix[5]*matrix[10] +
		matrix[12]*matrix[6]*matrix[9]

	inv[1] = -matrix[1]*matrix[10]*matrix[15] +
		matrix[1]*matrix[11]*matrix[14] +
		matrix[9]*matrix[2]*matrix[15] -
		matrix[9]*matrix[3]*matrix[14] -
		matrix[13]*matrix[2]*matrix[11] +
		matrix[13]*matrix[3]*matrix[10]

	inv[5] = matrix[0]*matrix[10]*matrix[15] -
		matrix[0]*matrix[11]*matrix[14] -
		matrix[8]*matrix[2]*matrix[15] +
		matrix[8]*matrix[3]*matrix[14] +
		matrix[12]*matrix[2]*matrix[11] -
		matrix[12]*matrix[3]*matrix[10]

	inv[9] = -matrix[0]*matrix[9]*matrix[15] +
		matrix[0]*matrix[11]*matrix[13] +
		matrix[8]*matrix[1]*matrix[15] -
		matrix[8]*matrix[3]*matrix[13] -
		matrix[12]*matrix[1]*matrix[11] +
		matrix[12]*matrix[3]*matrix[9]

	inv[13] = matrix[0]*matrix[9]*matrix[14] -
		matrix[0]*matrix[10]*matrix[13] -
		matrix[8]*matrix[1]*matrix[14] +
		matrix[8]*matrix[2]*matrix[13] +
		matrix[12]*matrix[1]*matrix[10] -
		matrix[12]*matrix[2]*matrix[9]

	inv[2] = matrix[1]*matrix[6]*matrix[15] -
		matrix[1]*matrix[7]*matrix[14] -
		matrix[5]*matrix[2]*matrix[15] +
		matrix[5]*matrix[3]*matrix[14] +
		matrix[13]*matrix[2]*matrix[7] -
		matrix[13]*matrix[3]*matrix[6]

	inv[6] = -matrix[0]*matrix[6]*matrix[15] +
		matrix[0]*matrix[7]*matrix[14] +
		matrix[4]*matrix[2]*matrix[15] -
		matrix[4]*matrix[3]*matrix[14] -
		matrix[12]*matrix[2]*matrix[7] +
		matrix[12]*matrix[3]*matrix[6]

	inv[10] = matrix[0]*matrix[5]*matrix[15] -
		matrix[0]*matrix[7]*matrix[13] -
		matrix[4]*matrix[1]*matrix[15] +
		matrix[4]*matrix[3]*matrix[13] +
		matrix[12]*matrix[1]*matrix[7] -
		matrix[12]*matrix[3]*matrix[5]

	inv[14] = -matrix[0]*matrix[5]*matrix[14] +
		matrix[0]*matrix[6]*matrix[13] +
		matrix[4]*matrix[1]*matrix[14] -
		matrix[4]*matrix[2]*matrix[13] -
		matrix[12]*matrix[1]*matrix[6] +
		matrix[12]*matrix[2]*matrix[5]

	inv[3] = -matrix[1]*matrix[6]*matrix[11] +
		matrix[1]*matrix[7]*matrix[10] +
		matrix[5]*matrix[2]*matrix[11] -
		matrix[5]*matrix[3]*matrix[10] -
		matrix[9]*matrix[2]*matrix[7] +
		matrix[9]*matrix[3]*matrix[6]

	inv[7] = matrix[0]*matrix[6]*matrix[11] -
		matrix[0]*matrix[7]*matrix[10] -
		matrix[4]*matrix[2]*matrix[11] +
		matrix[4]*matrix[3]*matrix[10] +
		matrix[8]*matrix[2]*matrix[7] -
		matrix[8]*matrix[3]*matrix[6]

	inv[11] = -matrix[0]*matrix[5]*matrix[11] +
		matrix[0]*matrix[7]*matrix[9] +
		matrix[4]*matrix[1]*matrix[11] -
		matrix[4]*matrix[3]*matrix[9] -
		matrix[8]*matrix[1]*matrix[7] +
		matrix[8]*matrix[3]*matrix[5]

	inv[15] = matrix[0]*matrix[5]*matrix[10] -
		matrix[0]*matrix[6]*matrix[9] -
		matrix[4]*matrix[1]*matrix[10] +
		matrix[4]*matrix[2]*matrix[9] +
		matrix[8]*matrix[1]*matrix[6] -
		matrix[8]*matrix[2]*matrix[5]

	det := matrix[0]*inv[0] + matrix[1]*inv[4] + matrix[2]*inv[8] + matrix[3]*inv[12]
	det = 1.0 / det

	for i := range inv {
		inv[i] *= det
	}
}

func cross(r *[3]float32, a, b [3]float32) {
	r[0] = a[1]*b[2] - a[2]*b[1]
	r[1] = a[2]*b[0] - a[0]*b[2]
	r[2] = a[0]*b[1] - a[1]*b[0]
}

func magnitude(r *[3]float32) float32 {
	return float32(math.Sqrt(float64(dot(r, r))))
}

func normalize(r *[3]float32) {
	len := magnitude(r)
	r[0] /= len
	r[1] /= len
	r[2] /= len
}

func dot(a, b *[3]float32) float32 {
	return a[0]*b[0] + a[1]*b[1] + a[2]*b[2]
}

func vector(a, b, c *[3]float32) {
	a[0] = b[0] - c[0]
	a[1] = b[1] - c[1]
	a[2] = b[2] - c[2]
}

func transformVector2(vec *[3]float32, m *[9]float32) {
	vec[0] = m[0]*vec[0] + m[1]*vec[1] + m[2]*vec[2]
	vec[1] = m[3]*vec[0] + m[4]*vec[1] + m[5]*vec[2]
	vec[2] = m[6]*vec[0] + m[7]*vec[1] + m[8]*vec[2]
}

func rotateX(vec *[3]float32, theta float32) {
	a := float32(math.Sin(float64(theta)))
	b := float32(math.Cos(float64(theta)))
	m := [9]float32{1, 0, 0, 0, b, -a, 0, a, b}
	transformVector2(vec, &m)
}

func rotateY(vec *[3]float32, theta float32) {
	a := float32(math.Sin(float64(theta)))
	b := float32(math.Cos(float64(theta)))
	m := [9]float32{b, 0, a, 0, 1, 0, -a, 0, b}
	transformVector2(vec, &m)
}

func rotateZ(vec *[3]float32, theta float32) {
	a := float32(math.Sin(float64(theta)))
	b := float32(math.Cos(float64(theta)))
	m := [9]float32{b, -a, 0, a, b, 0, 0, 0, 1}
	transformVector2(vec, &m)
}

func clamp(x, min, max float32) float32 {
	if x < min {
		return min
	}
	if x > max {
		return max
	}
	return x
}
