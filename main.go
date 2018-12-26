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
		glog.Infof("Unable to open device for streaming")
		os.Exit(1)
	}
	defer streamDevice.Close()

	streamCh, cancelCh := streamDevice.Stream()
	defer func() {
		cancelCh <- struct{}{}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	frameTimer := time.Tick(time.Nanosecond * 1000000000 / 30)
	frame := glitc.LTCFrame{FramesPerSecond: 30, DropFrame: true, ExternalClockSync: true}
	for {
		select {
		case t := <-frameTimer:
			frame.Time = t
			glog.Infof("Sending frame: %v", frame)
			for _, sample := range frame.GetAudioSamples(config.Rate, math.MaxInt32>>4) {
				streamCh <- sample
			}
		case <-c:
			goto cleanup
		}
	}
	//
	// glog.Infof("Config: %v", config)
	// glog.Infof("Streaming beep")
	// for i := 0; i < config.Rate*3; i++ {
	// 	t := float64(i) / float64(config.Rate)
	// 	streamCh <- int32(math.Sin(t*2*math.Pi) * math.MaxInt32)
	// }

cleanup:
	glog.Infof("Closing Stream")
	close(streamCh)
	for {
		glog.Info("Waiting for error or done")
		err, more := <-streamDevice.Done()
		if err != nil {
			glog.Infof("Error streaming data: %v", err)
		}
		if !more {
			glog.Info("Exiting")
			os.Exit(0)
		}
	}

}
