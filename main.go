package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"time"

	"github.com/azenk/ltcgen/glitc"
	"github.com/golang/glog"
	"github.com/yobert/alsa"
)

func main() {
	flag.Parse()

	cards, err := alsa.OpenCards()
	if err != nil {
		fmt.Println(err)
		return
	}
	defer alsa.CloseCards(cards)

	if len(cards) == 0 {
		glog.Infof("Unable to get alsa device")
		os.Exit(1)
	}

	card := cards[0]
	glog.Infof("Using card %v", card)

	devices, err := card.Devices()
	if err != nil {
		glog.Infof("Error getting devices: %v", err)
		os.Exit(1)
	}

	playbackDevice := &alsa.Device{}
	for _, dev := range devices {
		if dev.Type == alsa.PCM && dev.Play {
			playbackDevice = dev
			break
		}
	}

	glog.Infof("Found device %v", playbackDevice)

	streamDevice := NewStreamDevice(playbackDevice)
	config, err := streamDevice.Open()
	if err != nil {
		glog.Infof("Unable to open device for streaming: %v", err)
		os.Exit(1)
	}
	defer streamDevice.Close()
	glog.Infof("Device configuration -- %s", config)

	streamCh := streamDevice.Stream()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	frame := glitc.LTCFrame{FramesPerSecond: 30, DropFrame: true, ExternalClockSync: true}
	frameTimer := time.NewTicker(frame.FrameDuration())
	glog.Infof("Sending LTC frame every %s", frame.FrameDuration())
	outputDelay := config.OutputDelay()
	glog.Infof("Output delay estimated at %s, will attempt to compensate", outputDelay)
	for {
		select {
		case t := <-frameTimer.C:
			frame.Time = t.Add(outputDelay)
			delay := time.Since(frame.Time)
			for _, sample := range frame.GetAudioSamples(config.Rate, math.MaxInt32) {
				streamCh <- sample
			}
			glog.Infof("Sent frame for time %s: %v, Samples in queue: %d, delay: %s", frame.Time, frame.Frame(), len(streamCh), delay)
		case <-c:
			frameTimer.Stop()
			close(streamCh)
		case err, more := <-streamDevice.Done():
			if err != nil {
				glog.Infof("Error streaming data: %v", err)
			}

			if !more {
				glog.Info("Exiting")
				os.Exit(0)
			}
		}
	}
	//
	// glog.Infof("Config: %v", config)
	// glog.Infof("Streaming beep")
	// for i := 0; i < config.Rate*3; i++ {
	// 	t := float64(i) / float64(config.Rate)
	// 	streamCh <- int32(math.Sin(t*2*math.Pi) * math.MaxInt32)
	// }

}
