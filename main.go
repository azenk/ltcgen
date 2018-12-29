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
	"github.com/spf13/viper"
	"github.com/yobert/alsa"
)

func main() {
	flag.Parse()
	cfgFile := viper.New()
	cfgFile.AddConfigPath("/etc/ltcgen")
	cfgFile.SetConfigName("ltcgen")
	cfgFile.SetDefault("fps", 29.97)
	cfgFile.SetDefault("dropframe", true)
	cfgFile.ReadInConfig()

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

	fps := cfgFile.GetFloat64("fps")
	dropframe := cfgFile.GetBool("dropframe")

	if dropframe && fps != 29.97 {
		glog.Infof("Dropframe is set to true and isn't supported for the specified framerate.  Overriding fps to 29.97")
		fps = 29.97
	}

	frame := glitc.LTCFrame{FramesPerSecond: fps, DropFrame: dropframe, ExternalClockSync: true}
	glog.Infof("Configured for %f fps, dropframe: %v", frame.EffectiveFPS(), frame.DropFrame)

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
