package decoder

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"weather-dump/src/assets"
	"weather-dump/src/handlers/interfaces"

	SatHelper "github.com/OpenSatelliteProject/libsathelper"
	"github.com/fatih/color"
	"github.com/gorilla/websocket"
	"github.com/schollz/progressbar"
)

type CaduDecoder struct {
	hardData        []byte
	softData        []byte
	rsWorkBuffer    []byte
	rsCorrectedData []byte
	correlator      SatHelper.Correlator
	reedSolomon     SatHelper.ReedSolomon
	Statistics      assets.Statistics
	constSock       *websocket.Conn
	statsSock       *websocket.Conn
}

func NewCaduDecoder(uuid string) interfaces.Decoder {
	e := CaduDecoder{}

	if uuid != "" {
		http.HandleFunc(fmt.Sprintf("/hrd/%s/statistics", uuid), e.statistics)
	}

	e.softData = make([]byte, Datalink[id].FrameBits)
	e.hardData = make([]byte, Datalink[id].FrameSize)
	e.rsWorkBuffer = make([]byte, 255)
	e.rsCorrectedData = make([]byte, Datalink[id].FrameSize)
	e.correlator = SatHelper.NewCorrelator()
	e.reedSolomon = SatHelper.NewReedSolomon()
	e.reedSolomon.SetCopyParityToOutput(true)

	e.correlator.AddWord(uint(0x1ACFFC1D))
	e.correlator.AddWord(uint(0xE53003E2))

	return &e
}

func (e *CaduDecoder) Work(inputPath string, outputPath string, g *bool) {
	color.Red("[DEC] WARNING! This decoder is currently in ALPHA development state.")

	flywheelCount := 0
	isCorrupted := true

	input, err := os.Open(inputPath)
	if err != nil {
		fmt.Println(err)
		return
	}

	outputBuf, err := os.Create(outputPath + ".buf")
	if err != nil {
		fmt.Println(err)
		return
	}

	fi, _ := os.Stat(inputPath)
	bar := progressbar.NewOptions(int(fi.Size()))

	fmt.Println("[DEC] Converting CADU file:")
	bar.RenderBlank()

	for *g {
		n, err := input.Read(e.hardData)
		if Datalink[id].FrameSize != n {
			break
		}

		if err == nil {
			bar.Add(n)
			ConvertToArray(e.hardData, &e.softData, Datalink[id].FrameSize)
			outputBuf.Write(e.softData)
		} else {
			if err != io.EOF {
				fmt.Println(err)
			}
			break
		}
	}

	input.Close()
	outputBuf.Close()

	output, err := os.Create(outputPath)
	if err != nil {
		fmt.Println(err)
		return
	}

	inputBuf, err := os.Open(outputPath + ".buf")
	if err != nil {
		fmt.Println(err)
		return
	}

	fi, _ = os.Stat(outputPath + ".buf")
	bar = progressbar.NewOptions(int(fi.Size()))

	fmt.Println("\n[DEC] Decoding soft-symbol buffer file:")
	bar.RenderBlank()

	for *g {
		n, err := inputBuf.Read(e.softData)
		if Datalink[id].FrameBits != n {
			break
		}

		if err == nil {
			bar.Add(n)
			e.Statistics.TotalBytesRead += uint64(n)

			if flywheelCount == defaultFlywheelRecheck*8 {
				isCorrupted = true
				flywheelCount = 0
			}

			if isCorrupted {
				e.correlator.Correlate(&e.softData[0], uint(Datalink[id].FrameBits))
			} else {
				e.correlator.Correlate(&e.softData[0], uint(Datalink[id].FrameBits)/128)
				if e.correlator.GetHighestCorrelationPosition() != 0 {
					e.correlator.Correlate(&e.softData[0], uint(Datalink[id].FrameBits))
					flywheelCount = 0
				}
			}
			flywheelCount++

			pos := e.correlator.GetHighestCorrelationPosition()
			corr := e.correlator.GetHighestCorrelation()

			if corr < Datalink[id].MinCorrelationBits/2 {
				//fmt.Printf("[DEC] Not enough correlations %d/%d. Skipping...\n", corr, Datalink[id].MinCorrelationBits)
				continue
			}

			if pos != 0 {
				shiftWithConstantSize(&e.softData, int(pos), Datalink[id].FrameBits)
				offset := Datalink[id].FrameBits - int(pos)

				buffer := make([]byte, int(pos))
				n, err = inputBuf.Read(buffer)

				bar.Add(n)
				e.Statistics.TotalBytesRead += uint64(n)
				if err != nil {
					fmt.Println(err)
					break
				}

				for i := offset; i < Datalink[id].FrameBits; i++ {
					e.softData[i] = buffer[i-offset]
				}
			}

			for i := 0; i < Datalink[id].FrameBits; i += 8 {
				b := byte(0x00)
				for j := i; j < i+8 && j < Datalink[id].FrameBits; j++ {
					v := byte(0x00)
					if e.softData[j] > 128 {
						v = byte(0x01)
					}
					b = (b << 1) | v
				}
				e.hardData[i/8] = b
			}

			shiftWithConstantSize(&e.hardData, Datalink[id].SyncWordSize, Datalink[id].FrameSize-Datalink[id].SyncWordSize)
			SatHelper.DeRandomizerDeRandomize(&e.hardData[0], Datalink[id].FrameSize-Datalink[id].SyncWordSize)
			e.Statistics.TotalPackets++

			derrors := make([]int32, Datalink[id].RsBlocks)

			for i := 0; i < Datalink[id].RsBlocks; i++ {
				e.reedSolomon.Deinterleave(&e.hardData[0], &e.rsWorkBuffer[0], byte(i), byte(Datalink[id].RsBlocks))
				derrors[i] = int32(int8(e.reedSolomon.Decode_ccsds(&e.rsWorkBuffer[0])))
				e.reedSolomon.Interleave(&e.rsWorkBuffer[0], &e.rsCorrectedData[0], byte(i), byte(Datalink[id].RsBlocks))
				e.Statistics.RsErrors[i] = derrors[i]
			}

			if derrors[0] == -1 && derrors[1] == -1 && derrors[2] == -1 && derrors[3] == -1 {
				isCorrupted = true
				e.Statistics.DroppedPackets++
			} else {
				isCorrupted = false
			}

			scid := ((e.rsCorrectedData[0] & 0x3F) << 2) | (e.rsCorrectedData[1]&0xC0)>>6
			vcid := e.rsCorrectedData[1] & 0x3F
			counter := uint(e.rsCorrectedData[2])
			counter = SatHelper.ToolsSwapEndianess(counter)
			counter &= 0xFFFFFF00
			counter >>= 8

			e.Statistics.SCID = scid
			e.Statistics.VCID = vcid

			e.Statistics.PacketNumber = uint64(counter)
			e.Statistics.FrameBits = uint16(Datalink[id].FrameBits)
			e.Statistics.TotalBytes = uint64(fi.Size())

			if !isCorrupted {
				dat := e.rsCorrectedData[:Datalink[id].FrameSize-Datalink[id].RsParityBlockSize-Datalink[id].SyncWordSize]
				output.Write(dat)
			} else {
				e.Statistics.FrameLock = 0
			}

			if e.Statistics.TotalPackets%32 == 0 && e.statsSock != nil {
				e.updateStatistics(e.Statistics)
			}
		} else {
			if err != io.EOF {
				fmt.Println(err)
			}
			break
		}
	}

	output.Close()
	inputBuf.Close()
	os.Remove(outputPath + ".buf")

	if e.statsSock != nil {
		e.Statistics.Finish()
		e.updateStatistics(e.Statistics)
	}

	fmt.Printf("\n[DEC] Decoding finished! File saved as %s\n", outputPath)
}

func ConvertToArray(hard []byte, soft *[]byte, len int) {
	var buf = make([]bool, len*8)
	for i := 0; i < len; i++ {
		buf[0+8*i] = hard[i]>>7&0x01 == 0x01
		buf[1+8*i] = hard[i]>>6&0x01 == 0x01
		buf[2+8*i] = hard[i]>>5&0x01 == 0x01
		buf[3+8*i] = hard[i]>>4&0x01 == 0x01
		buf[4+8*i] = hard[i]>>3&0x01 == 0x01
		buf[5+8*i] = hard[i]>>2&0x01 == 0x01
		buf[6+8*i] = hard[i]>>1&0x01 == 0x01
		buf[7+8*i] = hard[i]>>0&0x01 == 0x01
	}
	for i := 0; i < len*8; i++ {
		if buf[i] == true {
			(*soft)[i] = 0xFF
		} else {
			(*soft)[i] = 0x00
		}
	}
}

func (e *CaduDecoder) updateStatistics(s assets.Statistics) {
	json, err := json.Marshal(s)
	if err == nil {
		e.statsSock.WriteMessage(1, []byte(json))
	}
}

func (e *CaduDecoder) statistics(w http.ResponseWriter, r *http.Request) {
	upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	e.statsSock, _ = upgrader.Upgrade(w, r, nil)
}

func shiftWithConstantSize(arr *[]byte, pos int, length int) {
	for i := 0; i < length-pos; i++ {
		(*arr)[i] = (*arr)[pos+i]
	}
}