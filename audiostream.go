package main

import (
	"bytes"
	"encoding/binary"

	"github.com/golang/glog"
	"github.com/yobert/alsa"
)

type StreamConfiguration struct {
	PeriodSize int
	BufferSize int
	Format     alsa.FormatType
	Rate       int
	Channels   int
}

type StreamDevice struct {
	device *alsa.Device
	doneCh chan error
	config *StreamConfiguration
}

func NewStreamDevice(device *alsa.Device) *StreamDevice {
	d := &StreamDevice{
		device: device,
		doneCh: make(chan error),
		config: &StreamConfiguration{},
	}
	return d
}

func (d *StreamDevice) Config() *StreamConfiguration {
	return d.config
}

func (d *StreamDevice) Open() (*StreamConfiguration, error) {
	var err error

	if err = d.device.Open(); err != nil {
		return nil, err
	}

	// Cleanup device when done or force cleanup after 3 seconds.

	channels, err := d.device.NegotiateChannels(1, 2)
	if err != nil {
		return nil, err
	}

	rate, err := d.device.NegotiateRate(44100)
	if err != nil {
		return nil, err
	}

	format, err := d.device.NegotiateFormat(alsa.S16_LE, alsa.S32_LE)
	if err != nil {
		return nil, err
	}

	// A 50ms period is a sensible value to test low-ish latency.
	// We adjust the buffer so it's of minimal size (period * 2) since it appear ALSA won't
	// start playback until the buffer has been filled to a certain degree and the automatic
	// buffer size can be quite large.
	// Some devices only accept even periods while others want powers of 2.
	wantPeriodSize := 2048 // 46ms @ 44100Hz

	periodSize, err := d.device.NegotiatePeriodSize(wantPeriodSize)
	if err != nil {
		return nil, err
	}

	bufferSize, err := d.device.NegotiateBufferSize(wantPeriodSize * 2)
	if err != nil {
		return nil, err
	}

	if err = d.device.Prepare(); err != nil {
		return nil, err
	}

	c := &StreamConfiguration{
		BufferSize: bufferSize,
		PeriodSize: periodSize,
		Format:     format,
		Rate:       rate,
		Channels:   channels,
	}
	d.config = c

	return c, nil
}

func (d *StreamDevice) Close() {
	glog.Info("Closing device")
	d.device.Close()
	glog.Info("Device Closed")
}

func (d *StreamDevice) Done() chan error {
	return d.doneCh
}

func (d *StreamDevice) Stream() (chan int32, chan struct{}) {
	cancelCh := make(chan struct{})
	sampleCh := make(chan int32, d.config.PeriodSize)

	go func(cancelCh chan struct{}) {
		var buf bytes.Buffer
		var samplesRead int
		for {
			var sample int32
			var more bool

			select {
			case sample, more = <-sampleCh:
				samplesRead++
				switch d.config.Format {
				case alsa.S16_LE:
					for c := 0; c < d.config.Channels; c++ {
						binary.Write(&buf, binary.LittleEndian, int16(sample>>16))
					}

				case alsa.S32_LE:
					for c := 0; c < d.config.Channels; c++ {
						binary.Write(&buf, binary.LittleEndian, sample)
					}
				}
			case <-cancelCh:
				d.doneCh <- nil
				close(d.doneCh)
				return
			}

			if samplesRead >= d.config.PeriodSize || !more {
				if err := d.device.Write(buf.Bytes(), d.config.PeriodSize); err != nil {
					d.doneCh <- err
				}
				samplesRead = 0
			}

			if !more {
				glog.Infof("Stream closed")
				d.doneCh <- nil
				close(d.doneCh)
				return
			}
		}
	}(cancelCh)

	return sampleCh, cancelCh
}
