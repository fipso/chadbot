package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	pb "github.com/fipso/chadbot/gen/chadbot"
	"github.com/fipso/chadbot/pkg/sdk"
)

//go:embed PLUGIN.md
var pluginDocumentation string

const (
	pluginName    = "vpd"
	pluginVersion = "1.0.0"
	pluginDesc    = "Calculate Vapor Pressure Deficit (VPD) and generate VPD charts"
)

var client *sdk.Client

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client = sdk.NewClient(pluginName, pluginVersion, pluginDesc)
	client = client.SetDocumentation(pluginDocumentation)

	// Register VPD calculation skill
	client.RegisterSkill(&pb.Skill{
		Name:        "calculate_vpd",
		Description: "Calculate the Vapor Pressure Deficit (VPD) given temperature and relative humidity",
		Parameters: []*pb.SkillParameter{
			{Name: "temperature", Type: "number", Description: "Air temperature in Celsius", Required: true},
			{Name: "humidity", Type: "number", Description: "Relative humidity as percentage (0-100)", Required: true},
			{Name: "leaf_offset", Type: "number", Description: "Leaf temperature offset from air temp in Celsius (default: -2)", Required: false},
		},
	}, handleCalculateVPD)

	// Register VPD chart generation skill with context (needs chat_id to add image)
	client.RegisterSkillWithContext(&pb.Skill{
		Name:        "vpd_chart",
		Description: "Generate a VPD chart as PNG image showing VPD zones with colored backgrounds across temperature and humidity ranges",
		Parameters: []*pb.SkillParameter{
			{Name: "min_temp", Type: "number", Description: "Minimum temperature in Celsius (default: 13)", Required: false},
			{Name: "max_temp", Type: "number", Description: "Maximum temperature in Celsius (default: 32)", Required: false},
			{Name: "min_humidity", Type: "number", Description: "Minimum humidity percentage (default: 20)", Required: false},
			{Name: "max_humidity", Type: "number", Description: "Maximum humidity percentage (default: 80)", Required: false},
			{Name: "current_temp", Type: "number", Description: "Current temperature to mark on chart", Required: false},
			{Name: "current_humidity", Type: "number", Description: "Current humidity to mark on chart", Required: false},
			{Name: "marker_label", Type: "string", Description: "Label for the marker (default: 'Current')", Required: false},
		},
	}, handleVPDChart)

	// Register VPD zone info skill
	client.RegisterSkill(&pb.Skill{
		Name:        "vpd_zones",
		Description: "Get information about VPD zones and their optimal ranges for plant growth",
		Parameters:  []*pb.SkillParameter{},
	}, handleVPDZones)

	if err := client.Connect(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect: %v\n", err)
		os.Exit(1)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		cancel()
	}()

	if err := client.Run(ctx); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "Plugin error: %v\n", err)
		os.Exit(1)
	}
}

// calculateSVP calculates Saturated Vapor Pressure using the Tetens formula
// Returns SVP in kPa
func calculateSVP(tempC float64) float64 {
	return 0.6108 * math.Exp((17.27*tempC)/(tempC+237.3))
}

// calculateVPD calculates Vapor Pressure Deficit
// tempC: air temperature in Celsius
// humidity: relative humidity as percentage (0-100)
// leafOffset: leaf temperature offset from air temp (typically -2°C)
// Returns VPD in kPa
func calculateVPD(tempC, humidity, leafOffset float64) float64 {
	leafTemp := tempC + leafOffset
	svpLeaf := calculateSVP(leafTemp)
	svpAir := calculateSVP(tempC)
	avp := svpAir * (humidity / 100.0)
	vpd := svpLeaf - avp
	if vpd < 0 {
		vpd = 0
	}
	return vpd
}

// getVPDZone returns the zone name for a given VPD value
func getVPDZone(vpd float64) string {
	switch {
	case vpd < 0.4:
		return "danger_low"
	case vpd < 0.8:
		return "early_veg"
	case vpd < 1.0:
		return "late_veg"
	case vpd < 1.2:
		return "early_flower"
	case vpd < 1.6:
		return "late_flower"
	default:
		return "danger_high"
	}
}

func handleCalculateVPD(ctx context.Context, args map[string]string) (string, error) {
	tempStr := args["temperature"]
	if tempStr == "" {
		return "", fmt.Errorf("temperature is required")
	}
	temp, err := strconv.ParseFloat(tempStr, 64)
	if err != nil {
		return "", fmt.Errorf("invalid temperature: %v", err)
	}

	humStr := args["humidity"]
	if humStr == "" {
		return "", fmt.Errorf("humidity is required")
	}
	humidity, err := strconv.ParseFloat(humStr, 64)
	if err != nil {
		return "", fmt.Errorf("invalid humidity: %v", err)
	}

	leafOffset := -2.0
	if offsetStr, ok := args["leaf_offset"]; ok && offsetStr != "" {
		leafOffset, err = strconv.ParseFloat(offsetStr, 64)
		if err != nil {
			return "", fmt.Errorf("invalid leaf_offset: %v", err)
		}
	}

	vpd := calculateVPD(temp, humidity, leafOffset)
	zone := getVPDZone(vpd)

	return fmt.Sprintf(`Air: %.1f°C | RH: %.1f%% | Leaf: %.1f°C
VPD: %.2f kPa | Zone: %s`, temp, humidity, temp+leafOffset, vpd, zone), nil
}

func handleVPDZones(ctx context.Context, args map[string]string) (string, error) {
	return `VPD Zones:
<0.6 kPa: Danger Low (mold risk) - Blue
0.6-1.0 kPa: Early Veg / Propagation - Deep Sky Blue
1.0-1.4 kPa: Optimal (late veg/early flower) - Lime Green
1.4-1.8 kPa: Late Flower - Gold
>1.8 kPa: Danger High (stress risk) - Red`, nil
}

// VPD zone colors - matching screen-app exactly (fully opaque)
var vpdZoneColors = []struct {
	maxVPD float64
	col    color.RGBA
}{
	{0.6, color.RGBA{0, 0, 255, 255}},       // Blue - danger low
	{1.0, color.RGBA{0, 191, 255, 255}},     // Deep Sky Blue - propagation/early veg
	{1.4, color.RGBA{50, 205, 50, 255}},     // Lime Green - late veg/early flower (optimal)
	{1.8, color.RGBA{255, 215, 0, 255}},     // Gold - late flower
	{999, color.RGBA{255, 0, 0, 255}},       // Red - danger high
}

func getVPDColor(vpd float64) color.RGBA {
	for _, zone := range vpdZoneColors {
		if vpd < zone.maxVPD {
			return zone.col
		}
	}
	return vpdZoneColors[len(vpdZoneColors)-1].col
}

// calculateVPDSimple matches screen-app formula (no leaf offset)
func calculateVPDSimple(temp, rh float64) float64 {
	es := 0.6108 * math.Exp((17.27*temp)/(temp+237.3))
	ea := es * rh / 100.0
	return es - ea
}

// getVPDZoneSimple returns zone name using screen-app thresholds
func getVPDZoneSimple(vpd float64) string {
	switch {
	case vpd < 0.6:
		return "danger_low"
	case vpd < 1.0:
		return "early_veg"
	case vpd < 1.4:
		return "optimal"
	case vpd < 1.8:
		return "late_flower"
	default:
		return "danger_high"
	}
}

func handleVPDChart(ctx context.Context, inv *sdk.SkillInvocation) (string, error) {
	args := inv.Args

	// Debug: log all arguments received
	fmt.Fprintf(os.Stderr, "[VPD] vpd_chart called with args: %+v\n", args)

	// Chart parameters with defaults
	minTemp := 13.0
	maxTemp := 32.0
	minHumidity := 20.0
	maxHumidity := 80.0
	var currentTemp, currentHumidity float64
	hasCurrentPoint := false
	markerLabel := "Current"

	// Parse optional parameters
	if v, ok := args["min_temp"]; ok && v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			minTemp = parsed
		}
	}
	if v, ok := args["max_temp"]; ok && v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			maxTemp = parsed
		}
	}
	if v, ok := args["min_humidity"]; ok && v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			minHumidity = parsed
		}
	}
	if v, ok := args["max_humidity"]; ok && v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			maxHumidity = parsed
		}
	}
	if v, ok := args["current_temp"]; ok && v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			currentTemp = parsed
			hasCurrentPoint = true
		}
	}
	if v, ok := args["current_humidity"]; ok && v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			currentHumidity = parsed
		}
	}
	if v, ok := args["marker_label"]; ok && v != "" {
		markerLabel = v
	}

	// Debug: log parsed values
	fmt.Fprintf(os.Stderr, "[VPD] hasCurrentPoint=%v, currentTemp=%.1f, currentHumidity=%.1f\n", hasCurrentPoint, currentTemp, currentHumidity)

	// Chart dimensions
	const (
		chartWidth  = 600
		chartHeight = 400
		marginLeft  = 60
		marginRight = 20
		marginTop   = 40
		marginBottom = 50
	)

	plotWidth := chartWidth - marginLeft - marginRight
	plotHeight := chartHeight - marginTop - marginBottom

	// Create image
	img := image.NewRGBA(image.Rect(0, 0, chartWidth, chartHeight))

	// Fill background with white
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)

	// Draw VPD zones (pixel by pixel for the plot area)
	// Matching screen-app logic exactly:
	// - Y-axis: temp increases downward (low temp at top, high temp at bottom)
	// - X-axis: humidity decreases left to right (high RH on left, low RH on right)
	for py := 0; py < plotHeight; py++ {
		for px := 0; px < plotWidth; px++ {
			// Screen-app formula: temp := minTemp + float64(y)*(maxTemp-minTemp)/float64(height)
			temp := minTemp + float64(py)*(maxTemp-minTemp)/float64(plotHeight)
			// Screen-app formula: rh := maxHumidity - float64(x)*(maxHumidity-minHumidity)/float64(width)
			rh := maxHumidity - float64(px)*(maxHumidity-minHumidity)/float64(plotWidth)

			vpd := calculateVPDSimple(temp, rh)
			col := getVPDColor(vpd)

			imgX := marginLeft + px
			imgY := marginTop + py
			img.Set(imgX, imgY, col)
		}
	}

	// Draw grid lines (lighter color)
	gridColor := color.RGBA{100, 100, 100, 100}

	// Horizontal grid lines (temperature) - temp increases downward
	tempStep := 5.0
	for t := math.Ceil(minTemp/tempStep) * tempStep; t <= maxTemp; t += tempStep {
		// y = (temp - minTemp) * height / (maxTemp - minTemp)
		py := marginTop + int(float64(plotHeight)*(t-minTemp)/(maxTemp-minTemp))
		for px := marginLeft; px < marginLeft+plotWidth; px++ {
			img.Set(px, py, gridColor)
		}
	}

	// Vertical grid lines (humidity) - humidity decreases left to right
	humidityStep := 10.0
	for h := math.Ceil(minHumidity/humidityStep) * humidityStep; h <= maxHumidity; h += humidityStep {
		// x = (maxHumidity - h) * width / (maxHumidity - minHumidity)
		px := marginLeft + int(float64(plotWidth)*(maxHumidity-h)/(maxHumidity-minHumidity))
		for py := marginTop; py < marginTop+plotHeight; py++ {
			img.Set(px, py, gridColor)
		}
	}

	// Draw border around plot area
	borderColor := color.RGBA{0, 0, 0, 255}
	for px := marginLeft; px <= marginLeft+plotWidth; px++ {
		img.Set(px, marginTop, borderColor)
		img.Set(px, marginTop+plotHeight, borderColor)
	}
	for py := marginTop; py <= marginTop+plotHeight; py++ {
		img.Set(marginLeft, py, borderColor)
		img.Set(marginLeft+plotWidth, py, borderColor)
	}

	// Draw marker if current values provided
	if hasCurrentPoint {
		// Calculate marker position - matching screen-app formula:
		// x = (maxHumidity - rh) * width / (maxHumidity - minHumidity)
		// y = (temp - minTemp) * height / (maxTemp - minTemp)
		markerX := marginLeft + int(float64(plotWidth)*(maxHumidity-currentHumidity)/(maxHumidity-minHumidity))
		markerY := marginTop + int(float64(plotHeight)*(currentTemp-minTemp)/(maxTemp-minTemp))

		// Draw white outline circle (like screen-app but with visibility)
		outerRadius := 10
		for dy := -outerRadius; dy <= outerRadius; dy++ {
			for dx := -outerRadius; dx <= outerRadius; dx++ {
				if dx*dx+dy*dy <= outerRadius*outerRadius {
					img.Set(markerX+dx, markerY+dy, color.RGBA{255, 255, 255, 255})
				}
			}
		}
		// Draw filled black circle (matching screen-app)
		radius := 8
		for dy := -radius; dy <= radius; dy++ {
			for dx := -radius; dx <= radius; dx++ {
				if dx*dx+dy*dy <= radius*radius {
					img.Set(markerX+dx, markerY+dy, color.RGBA{0, 0, 0, 255})
				}
			}
		}

		// Draw label with white background for visibility
		labelX := markerX + 15
		labelY := markerY + 5
		// Draw white background behind label
		labelWidth := len(markerLabel) * 7 // approximate width per character
		for dy := -10; dy <= 3; dy++ {
			for dx := -2; dx <= labelWidth; dx++ {
				img.Set(labelX+dx, labelY+dy, color.RGBA{255, 255, 255, 255})
			}
		}
		// Draw label text
		drawString(img, markerLabel, labelX, labelY, color.RGBA{0, 0, 0, 255})
	}

	// Draw axis labels
	textColor := color.RGBA{0, 0, 0, 255}

	// Title
	drawString(img, "VPD Chart - Temperature vs Humidity", chartWidth/2-120, 20, textColor)

	// Y-axis label (Temperature)
	drawString(img, "Temp", 5, chartHeight/2-20, textColor)
	drawString(img, "(C)", 5, chartHeight/2, textColor)

	// X-axis label (Humidity)
	drawString(img, "Relative Humidity (%)", chartWidth/2-60, chartHeight-10, textColor)

	// Y-axis tick labels (temperature) - temp increases downward
	for t := math.Ceil(minTemp/tempStep) * tempStep; t <= maxTemp; t += tempStep {
		py := marginTop + int(float64(plotHeight)*(t-minTemp)/(maxTemp-minTemp))
		label := fmt.Sprintf("%.0f", t)
		drawString(img, label, marginLeft-25, py+4, textColor)
	}

	// X-axis tick labels (humidity - reversed, high to low)
	for h := math.Ceil(minHumidity/humidityStep) * humidityStep; h <= maxHumidity; h += humidityStep {
		px := marginLeft + int(float64(plotWidth)*(maxHumidity-h)/(maxHumidity-minHumidity))
		label := fmt.Sprintf("%.0f", h)
		drawString(img, label, px-8, marginTop+plotHeight+15, textColor)
	}

	// Draw legend - matching screen-app thresholds
	legendY := marginTop + 10
	legendX := marginLeft + 10
	legendItems := []struct {
		label string
		col   color.RGBA
	}{
		{"<0.6 Danger Low", vpdZoneColors[0].col},
		{"0.6-1.0 Early Veg", vpdZoneColors[1].col},
		{"1.0-1.4 Optimal", vpdZoneColors[2].col},
		{"1.4-1.8 Late Flower", vpdZoneColors[3].col},
		{">1.8 Danger High", vpdZoneColors[4].col},
	}

	for i, item := range legendItems {
		y := legendY + i*14
		// Draw color box
		for dy := 0; dy < 10; dy++ {
			for dx := 0; dx < 10; dx++ {
				img.Set(legendX+dx, y+dy, item.col)
			}
		}
		// Draw label
		drawString(img, item.label, legendX+14, y+9, textColor)
	}

	// Encode to PNG
	buf := bytes.NewBuffer(nil)
	if err := png.Encode(buf, img); err != nil {
		return "", fmt.Errorf("failed to encode PNG: %v", err)
	}

	// Build text result
	textResult := fmt.Sprintf("VPD Chart (%.0f-%.0f°C, %.0f-%.0f%% RH)", minTemp, maxTemp, minHumidity, maxHumidity)

	if hasCurrentPoint {
		currentVPD := calculateVPDSimple(currentTemp, currentHumidity)
		zone := getVPDZoneSimple(currentVPD)
		textResult += fmt.Sprintf("\n%s: %.1f°C, %.0f%% RH → %.2f kPa (%s)", markerLabel, currentTemp, currentHumidity, currentVPD, zone)
	}

	// Return the image as a deferred attachment
	result := map[string]interface{}{
		"text": textResult,
		"__deferred_attachment__": map[string]interface{}{
			"role":         "plugin",
			"content":      "",
			"display_only": true,
			"attachment": map[string]interface{}{
				"type":      "image",
				"mime_type": "image/png",
				"data":      base64.StdEncoding.EncodeToString(buf.Bytes()),
			},
		},
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return textResult, nil
	}

	return string(resultJSON), nil
}

// drawString draws a string on the image using basicfont
func drawString(img *image.RGBA, s string, x, y int, col color.Color) {
	point := fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y)}
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(col),
		Face: basicfont.Face7x13,
		Dot:  point,
	}
	d.DrawString(s)
}
