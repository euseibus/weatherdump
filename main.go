package main

import (
	"fmt"
	"time"
	remoteHandler "weatherdump/src/handlers/remote"
	terminalHandler "weatherdump/src/handlers/terminal"
	"weatherdump/src/img"

	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

const startMessage = `__          __        _   _               _____                        
\ \        / /       | | | |             |  __ \                       
 \ \  /\  / /__  __ _| |_| |__   ___ _ __| |  | |_   _ _ __ ___  _ __  
  \ \/  \/ / _ \/ _' | __| '_ \ / _ \ '__| |  | | | | | '_ ' _ \| '_ \ 
   \  /\  /  __/ (_| | |_| | | |  __/ |  | |__| | |_| | | | | | | |_) |
    \/  \/ \___|\__,_|\__|_| |_|\___|_|  |_____/ \__,_|_| |_| |_| .__/ 
                                                                | |    
								|_|`

var version string

var (
	output = kingpin.Flag("output", "Custom output folder. Default is the current input file folder.").Default("").String()

	exportPNG  = kingpin.Flag("png", "export pictures as PNG").Default("false").Bool()
	exportJPEG = kingpin.Flag("jpeg", "export pictures as JPEG (disable: --no-jpeg)").Default("true").Bool()
	equalize   = kingpin.Flag("equalize", "apply histogram equalization to output (disable: --no-equalize)").Short('e').Default("true").Bool()
	invert     = kingpin.Flag("invert", "invert infrared pixels of output (disable: --no-invert)").Short('i').Default("true").Bool()
	flop       = kingpin.Flag("flop", "apply horizonal flip to output").Short('f').Default("false").Bool()

	hrd            = kingpin.Command("hrd", "Activate workflow for the HRD protocol (NOAA-20 & Suomi).")
	hrdDecoderType = hrd.Arg("decoder", "choose the decoder (Options: cadu, soft or none to bypass decoder)").Required().String()
	hrdInputFile   = hrd.Arg("file", "input file path").Required().ExistingFile()

	lrpt            = kingpin.Command("lrpt", "Activate workflow for the LRPT protocol (Meteor-MN2).")
	lrptDecoderType = lrpt.Arg("decoder", "choose the decoder (Options: soft or none to bypass decoder)").Required().String()
	lrptInputFile   = lrpt.Arg("file", "input file path").Required().ExistingFile()

	remote     = kingpin.Command("remote", "Activate the remote controll API.")
	remotePort = remote.Arg("port", "server listen port").Default("3000").String()
	clientPort = remote.Arg("client", "client port").Default("3002").String()
)

func main() {
	if version == "" {
		version = "Dev-Mode"
	}

	kingpin.CommandLine.HelpFlag.Short('h')
	kingpin.Version(version)

	datalink := kingpin.Parse()

	if datalink == "remote" {
		remoteHandler.New().Listen(*remotePort, *clientPort)
	}

	fmt.Println(startMessage)
	fmt.Println()

	wf := img.NewPipeline()

	wf.AddPipe("Equalize", *equalize)
	wf.AddPipe("Flop", *flop)
	wf.AddPipe("Invert", *invert)
	wf.AddPipe("ExportPNG", *exportPNG)
	wf.AddPipe("ExportJPEG", *exportJPEG)

	start := time.Now()
	fmt.Printf("[CLI] Version %s\n", version)
	terminalHandler.HandleInput(datalink, *lrptInputFile+*hrdInputFile, *output, *hrdDecoderType+*lrptDecoderType, wf)
	fmt.Printf("[CLI] Tasks finished in %s\n", time.Since(start))
}
