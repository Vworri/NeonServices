package display

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	DisplayWidth  = 800
	DisplayHeight = 480
)

var (
	ColorWhite = color.RGBA{R: 255, G: 255, B: 255, A: 255}
	ColorBlack = color.RGBA{R: 0, G: 0, B: 0, A: 255}
	ColorGray  = color.RGBA{R: 128, G: 128, B: 128, A: 255}
)

type Renderer struct{}

func NewRenderer() *Renderer {
	return &Renderer{}
}

// RenderCanvas draws the full 800x480 display image
func (r *Renderer) RenderCanvas(data *DisplayData) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, DisplayWidth, DisplayHeight))
	// 1. Fill pure white background
	draw.Draw(img, img.Bounds(), &image.Uniform{C: ColorWhite}, image.Point{}, draw.Src)

	// 2. Outer Border (clean 2px border)
	drawRect(img, 2, 2, DisplayWidth-3, DisplayHeight-3, ColorBlack, 2)

	// 3. Top Status / Header Bar
	drawHLine(img, 10, DisplayWidth-10, 36, ColorBlack, 2)
	drawText(img, 20, 24, fmt.Sprintf("reTerminal E1001 • OpenDisplay [%s]", data.DeviceID), ColorBlack)

	refreshBadge := fmt.Sprintf("[%s Refresh • %s]", strings.ToUpper(data.RefreshType), data.LastUpdated.Format("15:04:05"))
	drawText(img, DisplayWidth-270, 24, refreshBadge, ColorBlack)

	// 4. Left Pane: Local Time & Date (x: 20..380)
	drawTimeLarge(img, 30, 60, data.TimeStr)

	drawTextScaled(img, 30, 160, data.DayOfWeek, ColorBlack, 2)
	drawText(img, 30, 195, data.DateStr, ColorBlack)
	drawText(img, 30, 220, fmt.Sprintf("📍 %s", data.CityName), ColorBlack)

	// Vertical Separator
	drawVLine(img, 395, 45, 245, ColorBlack, 2)

	// 5. Right Pane: Local Weather & Air Quality (x: 410..780)
	// Weather Box
	drawRect(img, 410, 48, DisplayWidth-20, 145, ColorBlack, 1)
	weatherHeader := fmt.Sprintf("WEATHER • %s", data.CityName)
	drawText(img, 425, 68, weatherHeader, ColorBlack)

	tempStr := fmt.Sprintf("%.0f%s", data.Weather.Temperature, data.Weather.TempUnit)
	drawTextScaled(img, 425, 90, tempStr, ColorBlack, 3)

	conditionStr := fmt.Sprintf("%s", data.Weather.Condition)
	drawText(img, 570, 95, conditionStr, ColorBlack)

	highLowStr := fmt.Sprintf("High: %.0f%s | Low: %.0f%s", data.Weather.TempMax, data.Weather.TempUnit, data.Weather.TempMin, data.Weather.TempUnit)
	drawText(img, 570, 115, highLowStr, ColorBlack)

	statsStr := fmt.Sprintf("Humidity: %d%% | Wind: %.1f mph", data.Weather.Humidity, data.Weather.WindSpeed)
	drawText(img, 570, 135, statsStr, ColorBlack)

	// Air Quality Box
	drawRect(img, 410, 155, DisplayWidth-20, 245, ColorBlack, 1)
	drawText(img, 425, 175, "AIR QUALITY INDEX (AQI)", ColorBlack)

	aqiBadge := fmt.Sprintf("AQI %d • %s", data.AirQuality.AQI, strings.ToUpper(data.AirQuality.Level))
	drawTextScaled(img, 425, 190, aqiBadge, ColorBlack, 2)

	pmStr := fmt.Sprintf("PM2.5: %.1f µg/m³  •  PM10: %.1f µg/m³", data.AirQuality.PM25, data.AirQuality.PM10)
	drawText(img, 425, 230, pmStr, ColorBlack)

	// Middle Horizontal Separator
	drawHLine(img, 10, DisplayWidth-10, 258, ColorBlack, 2)

	// 6. Bottom Pane: Upcoming Calendar & Schedule (y: 265..465)
	drawText(img, 25, 280, "📅 UPCOMING SCHEDULE & CALENDAR EVENTS", ColorBlack)

	eventsY := 305
	for i, e := range data.Events {
		if i >= 4 {
			break
		}
		timeRange := fmt.Sprintf("%s - %s", e.StartTime.Format("15:04"), e.EndTime.Format("15:04"))
		if e.IsAllDay {
			timeRange = "ALL DAY"
		}

		// Draw bullet
		drawFilledCircle(img, 30, eventsY-4, 3, ColorBlack)

		// Draw event row
		drawText(img, 42, eventsY, timeRange, ColorBlack)
		drawText(img, 170, eventsY, e.Title, ColorBlack)
		if e.Location != "" {
			drawText(img, 520, eventsY, fmt.Sprintf("(%s)", e.Location), ColorBlack)
		}

		// Separator line between events
		if i < len(data.Events)-1 && i < 3 {
			drawHLine(img, 30, DisplayWidth-30, eventsY+12, ColorGray, 1)
		}
		eventsY += 36
	}

	return img
}

// RenderPNG encodes canvas to PNG format
func (r *Renderer) RenderPNG(data *DisplayData) ([]byte, error) {
	img := r.RenderCanvas(data)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// RenderBMP creates a standard uncompressed 800x480 BMP image
func (r *Renderer) RenderBMP(data *DisplayData) ([]byte, error) {
	img := r.RenderCanvas(data)
	var buf bytes.Buffer

	// BMP Header for 800x480 24-bit
	width := DisplayWidth
	height := DisplayHeight
	rowSize := (width*3 + 3) &^ 3
	imageSize := rowSize * height
	fileSize := 54 + imageSize

	// File Header (14 bytes)
	buf.Write([]byte("BM"))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(fileSize))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0)) // reserved
	_ = binary.Write(&buf, binary.LittleEndian, uint32(54)) // pixel array offset

	// DIB Header (40 bytes)
	_ = binary.Write(&buf, binary.LittleEndian, uint32(40))
	_ = binary.Write(&buf, binary.LittleEndian, int32(width))
	_ = binary.Write(&buf, binary.LittleEndian, int32(height))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(24))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0)) // uncompressed
	_ = binary.Write(&buf, binary.LittleEndian, uint32(imageSize))
	_ = binary.Write(&buf, binary.LittleEndian, int32(2835)) // ~72 DPI
	_ = binary.Write(&buf, binary.LittleEndian, int32(2835))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0))

	// Pixel rows bottom-to-top in standard BMP
	row := make([]byte, rowSize)
	for y := height - 1; y >= 0; y-- {
		idx := 0
		for x := 0; x < width; x++ {
			c := img.RGBAAt(x, y)
			row[idx] = c.B
			row[idx+1] = c.G
			row[idx+2] = c.R
			idx += 3
		}
		buf.Write(row)
	}

	return buf.Bytes(), nil
}

// Render1BitRaw packs 800x480 monochrome pixels into 1 bit per pixel (48,000 bytes)
// 1 = White, 0 = Black (standard e-Paper framebuffer)
func (r *Renderer) Render1BitRaw(data *DisplayData) []byte {
	img := r.RenderCanvas(data)
	raw := make([]byte, (DisplayWidth*DisplayHeight)/8)

	for y := 0; y < DisplayHeight; y++ {
		for x := 0; x < DisplayWidth; x++ {
			c := img.RGBAAt(x, y)
			// Threshold: > 128 is white (1), <= 128 is black (0)
			isWhite := (int(c.R)+int(c.G)+int(c.B))/3 > 128
			if isWhite {
				byteIdx := (y*DisplayWidth + x) / 8
				bitOffset := 7 - (x % 8)
				raw[byteIdx] |= (1 << bitOffset)
			}
		}
	}
	return raw
}

// --- Drawing Helper Functions ---

func drawText(img *image.RGBA, x, y int, label string, col color.RGBA) {
	d := &font.Drawer{
		Dst:  img,
		Src:  &image.Uniform{C: col},
		Face: basicfont.Face7x13,
		Dot:  fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y)},
	}
	d.DrawString(label)
}

func drawTextScaled(img *image.RGBA, x, y int, label string, col color.RGBA, scale int) {
	if scale <= 1 {
		drawText(img, x, y, label, col)
		return
	}

	// Render to small temporary canvas then scale pixels
	tempW := len(label) * 8 + 10
	tempH := 18
	temp := image.NewRGBA(image.Rect(0, 0, tempW, tempH))
	d := &font.Drawer{
		Dst:  temp,
		Src:  &image.Uniform{C: col},
		Face: basicfont.Face7x13,
		Dot:  fixed.Point26_6{X: fixed.I(0), Y: fixed.I(13)},
	}
	d.DrawString(label)

	for sy := 0; sy < tempH; sy++ {
		for sx := 0; sx < tempW; sx++ {
			if temp.RGBAAt(sx, sy).A > 64 {
				for dy := 0; dy < scale; dy++ {
					for dx := 0; dx < scale; dx++ {
						px := x + sx*scale + dx
						py := y + sy*scale + dy
						if px >= 0 && px < DisplayWidth && py >= 0 && py < DisplayHeight {
							img.Set(px, py, col)
						}
					}
				}
			}
		}
	}
}

// drawTimeLarge draws bold, crisp 60px digital clock numbers
func drawTimeLarge(img *image.RGBA, startX, startY int, timeStr string) {
	scale := 4
	drawTextScaled(img, startX, startY, timeStr, ColorBlack, scale)
}

func drawHLine(img *image.RGBA, x1, x2, y int, col color.RGBA, thickness int) {
	for t := 0; t < thickness; t++ {
		for x := x1; x <= x2; x++ {
			if x >= 0 && x < DisplayWidth && y+t >= 0 && y+t < DisplayHeight {
				img.Set(x, y+t, col)
			}
		}
	}
}

func drawVLine(img *image.RGBA, x, y1, y2 int, col color.RGBA, thickness int) {
	for t := 0; t < thickness; t++ {
		for y := y1; y <= y2; y++ {
			if x+t >= 0 && x+t < DisplayWidth && y >= 0 && y < DisplayHeight {
				img.Set(x+t, y, col)
			}
		}
	}
}

func drawRect(img *image.RGBA, x1, y1, x2, y2 int, col color.RGBA, thickness int) {
	drawHLine(img, x1, x2, y1, col, thickness)
	drawHLine(img, x1, x2, y2, col, thickness)
	drawVLine(img, x1, y1, y2, col, thickness)
	drawVLine(img, x2, y1, y2, col, thickness)
}

func drawFilledCircle(img *image.RGBA, cx, cy, r int, col color.RGBA) {
	for y := -r; y <= r; y++ {
		for x := -r; x <= r; x++ {
			if x*x+y*y <= r*r {
				px := cx + x
				py := cy + y
				if px >= 0 && px < DisplayWidth && py >= 0 && py < DisplayHeight {
					img.Set(px, py, col)
				}
			}
		}
	}
}
