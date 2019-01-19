package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/azenk/audio/stream"
	"github.com/azenk/audio/stream/encoding"

	"github.com/azenk/ltcgen/glitc"
	"github.com/golang/glog"
	"github.com/spf13/viper"
)

func main() {
	flag.Parse()
	cfgFile := viper.New()
	cfgFile.AddConfigPath("/etc/ltcgen")
	cfgFile.SetConfigName("ltcgen")
	cfgFile.SetDefault("fps", 29.97)
	cfgFile.SetDefault("dropframe", true)
	cfgFile.SetDefault("rateWindowMinutes", 2)
	cfgFile.SetDefault("pid.p", 1)
	cfgFile.SetDefault("pid.i", 1)
	cfgFile.SetDefault("pid.d", 1)
	cfgFile.SetDefault("pid.depth", 30)
	cfgFile.ReadInConfig()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	glog.Infof("Opening audio device")
	streamDevice, err := stream.OpenDefaultDevice(ctx, &stream.Configuration{Channels: 1})
	if err != nil {
		fmt.Println(err)
		return
	}
	glog.Infof("Device configuration -- %s", streamDevice.Config())

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt)
	signal.Notify(signalCh, syscall.SIGTERM)

	fps := cfgFile.GetFloat64("fps")
	dropframe := cfgFile.GetBool("dropframe")

	if dropframe && fps != 29.97 {
		glog.Infof("Dropframe is set to true and isn't supported for the specified framerate.  Overriding fps to 29.97")
		fps = 29.97
	}

	frame := glitc.LTCFrame{FramesPerSecond: fps, DropFrame: dropframe, ExternalClockSync: true}
	glog.Infof("Configured for %f fps, dropframe: %v", frame.EffectiveFPS(), frame.DropFrame)

	// override sample rate from config file
	sampleRate := float64(streamDevice.Config().SampleRate())
	if val := cfgFile.GetFloat64("samplerate"); val != 0 {
		sampleRate = val
		glog.Infof("Got sample rate from configuration file: %f", val)
	}

	// Set up manchester encoder
	rawFrameChan := make(chan byte, 160)
	samplesPerFrame := streamDevice.Config().SampleRate() / int(frame.EffectiveFPS())
	encodedData := encoding.DifferentialManchester(context.Background(),
		3*samplesPerFrame,
		frame.EffectiveFPS()*80,
		1.0,
		sampleRate,
		rawFrameChan)

	// Copy manchester encoded frames to streamDevice for output
	streamCh := streamDevice.Stream()
	go func() {
		for sample := range encodedData {
			streamCh <- []stream.Sample{sample}
		}
		close(streamCh)
	}()

	// Start Status Ticker
	statusTick := time.NewTicker(10 * time.Second)

	outputDelay := streamDevice.Config().OutputDelay()
	glog.Infof("Output delay estimated at %s, will attempt to compensate", outputDelay)

	// Calculate the time we should start our frame timing ticker
	frameDuration := frame.FrameDuration()
	frame.Time = time.Now()
	glog.Infof("Sync time %s", frame.Frame())
	syncTime := frame.FrameBeginTime().Add(2 * frameDuration).Add(-1 * outputDelay).Add(250 * time.Microsecond)
	syncTimer := time.NewTimer(time.Until(syncTime))
	glog.Infof("Waiting for next frame to start at: %s", syncTime)
	<-syncTimer.C
	frameTimer := time.NewTicker(frameDuration)
	// Set prevFrameIndex to now, this should be one frame before the first frame output
	frame.Time = time.Now().Add(outputDelay)
	var prevFrameIndex int = frame.FrameIndex()
	frame.Time = time.Now().Add(frameDuration).Add(outputDelay)
	glog.Infof("Sending LTC frame every %s, first frame should be %s", frameDuration, frame.Frame())

	status := NewStatus(int(frame.EffectiveFPS() * float64(60) * cfgFile.GetFloat64("rateWindowMinutes")))
	for {
		select {
		case t := <-frameTimer.C:
			frame.Time = t.Add(outputDelay)

			intraFrameOffset := time.Now().Add(outputDelay).Sub(frame.FrameBeginTime())
			// if intraFrameOffset > outputDelay/2 || intraFrameOffset <= time.Duration(0) {
			// 	glog.Infof("WARNING: current intra frame offset outside stream output buffer window: %s", intraFrameOffset)
			// }

			thisFrameIndex := frame.FrameIndex()
			if prevFrameIndex != 0 && thisFrameIndex != prevFrameIndex+1 {
				glog.Infof("WARNING: Frame error detected: current intra frame offset: %s", intraFrameOffset)
				if thisFrameIndex == prevFrameIndex {
					glog.Infof("WARNING: Would have output duplicate frame number at %s, skipping", frame.Frame())
					status.Duplicate()
					continue
				}
				glog.Infof("WARNING: Skipped %d frames at %s", thisFrameIndex-(prevFrameIndex+1), frame.Frame())
				status.Dropped(thisFrameIndex - (prevFrameIndex + 1))
			}

			for _, b := range frame.EncodeFrame() {
				rawFrameChan <- b
			}
			status.Sent(intraFrameOffset)

			prevFrameIndex = thisFrameIndex
		case <-statusTick.C:
			glog.Infof("%s", status)
		case <-signalCh:
			frameTimer.Stop()
			close(rawFrameChan)
		case err, more := <-streamDevice.Done():
			if err != nil {
				glog.Infof("Error streaming data: %v", err)
			}

			if !more {
				glog.Infof("%v", status)
				glog.Info("Exiting")
				os.Exit(0)
			}
		}
	}

}
