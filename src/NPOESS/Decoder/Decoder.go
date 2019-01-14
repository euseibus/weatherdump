package Decoder

import (
	"fmt"
	"io"
	"os"

	SatHelper "github.com/OpenSatelliteProject/libsathelper"
)

const DefaultFlywheelRecheck = 4
const AverageLastNSamples = 10000
const ID = "HRD"

type Decoder struct {
	viterbiData     []byte
	decodedData     []byte
	codedData       []byte
	rsCorrectedData []byte
	rsWorkBuffer    []byte
	syncWord        []byte
	viterbi         SatHelper.Viterbi27
	reedSolomon     SatHelper.ReedSolomon
	correlator      SatHelper.Correlator
	packetFixer     SatHelper.PacketFixer
	Statistics      Statistics
}

func NewDecoder() *Decoder {
	e := Decoder{}

	e.viterbiData = make([]byte, Datalink[ID].CodedFrameSize)
	e.decodedData = make([]byte, Datalink[ID].FrameSize)

	e.viterbi = SatHelper.NewViterbi27(Datalink[ID].FrameBits)

	e.codedData = make([]byte, Datalink[ID].CodedFrameSize)
	e.rsCorrectedData = make([]byte, Datalink[ID].FrameSize)
	e.rsWorkBuffer = make([]byte, 255)

	e.reedSolomon = SatHelper.NewReedSolomon()
	e.correlator = SatHelper.NewCorrelator()
	e.packetFixer = SatHelper.NewPacketFixer()

	e.syncWord = make([]byte, 4)

	e.reedSolomon.SetCopyParityToOutput(true)

	e.correlator.AddWord(Datalink[ID].HritUw0)
	e.correlator.AddWord(Datalink[ID].HritUw2)

	return &e
}

func (e *Decoder) DecodeFile() {
	var isCorrupted bool
	lastFrameOk := false

	var averageRSCorrections float32 = 0.0
	var averageVitCorrections float32 = 0.0
	var lostPacketsPerChannel [256]int64
	var lastPacketCount [256]int64
	var receivedPacketsPerChannel [256]int64
	var flywheelCount = 0

	fmt.Printf("[DECODER] Opening files...\n")

	input, err := os.Open("/home/luigifcruz/Sandbox/osp/arved.raw")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer input.Close()

	output, err := os.Create("./output/demod.bin")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer output.Close()

	for {
		n, err := input.Read(e.codedData)
		e.Statistics.TotalBytesRead += uint64(n)

		if err == nil {
			if e.Statistics.TotalPackets%AverageLastNSamples == 0 {
				averageRSCorrections = 0
				averageVitCorrections = 0
			}

			if flywheelCount == DefaultFlywheelRecheck {
				lastFrameOk = false
				flywheelCount = 0
			}

			if !lastFrameOk {
				e.correlator.Correlate(&e.codedData[0], uint(Datalink[ID].CodedFrameSize))
			} else {
				e.correlator.Correlate(&e.codedData[0], uint(Datalink[ID].CodedFrameSize)/16)
				if e.correlator.GetHighestCorrelationPosition() != 0 {
					e.correlator.Correlate(&e.codedData[0], uint(Datalink[ID].CodedFrameSize))
					flywheelCount = 0
				}
			}
			flywheelCount++

			pos := e.correlator.GetHighestCorrelationPosition()
			corr := e.correlator.GetHighestCorrelation()

			if corr < Datalink[ID].MinCorrelationBits {
				fmt.Printf("Correlation didn't match criteria of %d bits. Got %d\n", Datalink[ID].MinCorrelationBits, corr)
			}

			if pos != 0 {
				// Sync frame
				shiftWithConstantSize(&e.codedData, int(pos), Datalink[ID].CodedFrameSize)
				offset := Datalink[ID].CodedFrameSize - int(pos)

				buffer := make([]byte, int(pos))
				n, err = input.Read(buffer)

				e.Statistics.TotalBytesRead += uint64(n)
				if err != nil {
					fmt.Println(err)
					break
				}

				for i := offset; i < Datalink[ID].CodedFrameSize; i++ {
					e.codedData[i] = buffer[i-offset]
				}
			}

			for i := 0; i < Datalink[ID].CodedFrameSize; i++ {
				e.viterbiData[i] = e.codedData[i]
			}

			e.viterbi.Decode(&e.viterbiData[0], &e.decodedData[0])

			nrzmDecodeSize := Datalink[ID].FrameSize

			SatHelper.DifferentialEncodingNrzmDecode(&e.decodedData[0], nrzmDecodeSize)

			signalErrors := float32(e.viterbi.GetPercentBER())
			signalErrors = 100 - (signalErrors * 10)
			signalQuality := uint8(signalErrors)

			if signalQuality > 100 {
				signalQuality = 0
			}

			averageVitCorrections += float32(e.viterbi.GetBER())

			for i := 0; i < Datalink[ID].SyncWordSize; i++ {
				e.syncWord[i] = e.decodedData[i]
				e.Statistics.SyncWord[i] = e.decodedData[i]
			}

			shiftWithConstantSize(&e.decodedData, Datalink[ID].SyncWordSize, Datalink[ID].FrameSize-Datalink[ID].SyncWordSize)

			e.Statistics.AverageVitCorrections += uint16(e.viterbi.GetBER())
			e.Statistics.TotalPackets += 1

			SatHelper.DeRandomizerDeRandomize(&e.decodedData[0], Datalink[ID].FrameSize-Datalink[ID].SyncWordSize)

			derrors := make([]int32, Datalink[ID].RsBlocks)

			for i := 0; i < Datalink[ID].RsBlocks; i++ {
				e.reedSolomon.Deinterleave(&e.decodedData[0], &e.rsWorkBuffer[0], byte(i), byte(Datalink[ID].RsBlocks))
				derrors[i] = int32(int8(e.reedSolomon.Decode_ccsds(&e.rsWorkBuffer[0])))
				e.reedSolomon.Interleave(&e.rsWorkBuffer[0], &e.rsCorrectedData[0], byte(i), byte(Datalink[ID].RsBlocks))
				if derrors[i] != -1 {
					averageRSCorrections += float32(derrors[i])
				}
				e.Statistics.RsErrors[i] = derrors[i]
			}

			if derrors[0] == -1 && derrors[1] == -1 && derrors[2] == -1 && derrors[3] == -1 {
				isCorrupted = true
				lastFrameOk = false
				e.Statistics.DroppedPackets += 1
			} else {
				isCorrupted = false
				lastFrameOk = true
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
			e.Statistics.VitErrors = uint16(e.viterbi.GetBER())
			e.Statistics.FrameBits = uint16(Datalink[ID].FrameBits)
			e.Statistics.SignalQuality = signalQuality
			e.Statistics.SyncCorrelation = uint8(corr)

			if !isCorrupted {
				if lastPacketCount[vcid]+1 != int64(counter) && lastPacketCount[vcid] > -1 {
					lostCount := int(int64(counter) - lastPacketCount[vcid] - 1)
					e.Statistics.LostPackets += uint64(lostCount)
					lostPacketsPerChannel[vcid] += int64(lostCount)
				}
				lastPacketCount[vcid] = int64(counter)
				if receivedPacketsPerChannel[vcid] == -1 {
					receivedPacketsPerChannel[vcid] = 1
				} else {
					receivedPacketsPerChannel[vcid] = receivedPacketsPerChannel[vcid] + 1
				}

				if e.Statistics.TotalPackets%AverageLastNSamples == 0 {
					e.Statistics.AverageRSCorrections = uint8(averageRSCorrections / 4)
					e.Statistics.AverageVitCorrections = uint16(averageVitCorrections)
				} else {
					e.Statistics.AverageRSCorrections = uint8(averageRSCorrections / float32(4*(e.Statistics.TotalPackets%AverageLastNSamples)))
					e.Statistics.AverageVitCorrections = uint16(averageVitCorrections / float32(e.Statistics.TotalPackets%AverageLastNSamples))
				}
				e.Statistics.FrameLock = 1
				for i := 0; i < 256; i++ {
					e.Statistics.ReceivedPacketsPerChannel[i] = receivedPacketsPerChannel[i]
					e.Statistics.LostPacketsPerChannel[i] = lostPacketsPerChannel[i]
				}

				dat := e.rsCorrectedData[:Datalink[ID].FrameSize-Datalink[ID].RsParityBlockSize-Datalink[ID].SyncWordSize]
				output.Write(dat)
			} else {
				e.Statistics.FrameLock = 0
			}

			fmt.Printf("(%d)\nAverageVitCorrections: %d\nAverageRSCorrections: %d\nSignalQuality: %d\nTotalBytesRead: %d\nDroppedPackages: %d/%d\n",
				e.Statistics.FrameLock, e.Statistics.AverageVitCorrections, derrors[0], e.Statistics.SignalQuality,
				e.Statistics.TotalBytesRead, e.Statistics.DroppedPackets, e.Statistics.TotalPackets)
		} else {
			if err != io.EOF {
				fmt.Println(err)
			}
			break
		}
	}
}

func shiftWithConstantSize(arr *[]byte, pos int, length int) {
	for i := 0; i < length-pos; i++ {
		(*arr)[i] = (*arr)[pos+i]
	}
}