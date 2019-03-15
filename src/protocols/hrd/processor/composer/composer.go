package composer

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"weather-dump/src/protocols/hrd"
	"weather-dump/src/protocols/hrd/processor/parser"
	"weather-dump/src/tools/img"
)

type Composer struct {
	pipeline         img.Pipeline
	scft             hrd.SpacecraftParameters
	RequiredChannels []uint16
}

func (e *Composer) Register(pipeline img.Pipeline, scft hrd.SpacecraftParameters) *Composer {
	e.pipeline = pipeline
	e.scft = scft
	return e
}

func (e Composer) Render(ch parser.ChannelList, outputFolder string) {
	fmt.Println("[COM] Exporting true color channel.")

	ch01 := ch[e.RequiredChannels[0]]
	ch02 := ch[e.RequiredChannels[1]]
	ch03 := ch[e.RequiredChannels[2]]

	// Check if required channels exist.
	if !ch01.HasData || !ch02.HasData || !ch03.HasData {
		fmt.Println("[COM] Can't export true color channel. Not all required channels are available.")
		return
	}

	// Synchronize all channels scans.
	firstScan := make([]int, 3)
	lastScan := make([]int, 3)

	firstScan[0], lastScan[0] = ch01.GetBounds()
	firstScan[1], lastScan[1] = ch02.GetBounds()
	firstScan[2], lastScan[2] = ch03.GetBounds()

	ch01.SetBounds(MaxIntSlice(firstScan), MinIntSlice(lastScan))
	ch02.SetBounds(MaxIntSlice(firstScan), MinIntSlice(lastScan))
	ch03.SetBounds(MaxIntSlice(firstScan), MinIntSlice(lastScan))

	ch01.Process(e.scft)
	ch02.Process(e.scft)
	ch03.Process(e.scft)

	// Create output image struct.
	w, h := ch01.GetDimensions()
	tmp := image.NewRGBA64(image.Rect(0, 0, w, h))
	bufferSize := w * h * 8
	finalImage := make([]byte, bufferSize)

	for p := 6; p < bufferSize; p += 8 {
		finalImage[p+0] = 0xFF
		finalImage[p+1] = 0xFF
	}

	// Compose images and fill buffer.
	buf := make([]byte, w*h*2)

	e.pipeline.AddException("Invert", false)

	ch01.Export(&buf, ch, e.scft)
	e.pipeline.Target(img.NewGray16(&buf, w, h)).Process()

	for p := 2; p < bufferSize; p += 8 {
		finalImage[p+0] = buf[(p/4)+0]
		finalImage[p+1] = buf[(p/4)+1]
	}

	ch02.Export(&buf, ch, e.scft)
	e.pipeline.Target(img.NewGray16(&buf, w, h)).Process()

	for p := 0; p < bufferSize; p += 8 {
		finalImage[p+0] = buf[(p/4)+0]
		finalImage[p+1] = buf[(p/4)+1]
	}

	ch03.Export(&buf, ch, e.scft)
	e.pipeline.Target(img.NewGray16(&buf, w, h)).Process()

	for p := 4; p < bufferSize; p += 8 {
		finalImage[p+0] = buf[(p/4)-1]
		finalImage[p+1] = buf[(p/4)+0]
	}

	e.pipeline.ResetExceptions()

	// Render and save the true-color image.
	tmp.Pix = finalImage
	outputName, _ := filepath.Abs(fmt.Sprintf("%s/TRUECOLOR_VIIRS_%s.png", outputFolder, ch01.StartTime.GetZuluSafe()))
	outputFile, err := os.Create(outputName)
	if err != nil {
		fmt.Println("[EXPORT] Error saving final image...")
	}
	png.Encode(outputFile, tmp)
	outputFile.Close()
}

func MinIntSlice(v []int) int {
	sort.Ints(v)
	return v[0]
}

func MaxIntSlice(v []int) int {
	sort.Ints(v)
	return v[len(v)-1]
}
