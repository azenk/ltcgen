package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"

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

func (c StreamConfiguration) SampleSizeBytes() int {
	switch c.Format {
	case alsa.S16_LE:
		return 2
	case alsa.S32_LE:
		return 4
	}
	return 0
}

func (c StreamConfiguration) String() string {
	return fmt.Sprintf("Period: %d, SampleSize: %d bits, Rate: %d HZ, Channels: %d", c.PeriodSize, c.SampleSizeBytes()*8, c.Rate, c.Channels)
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
	wantPeriodSize := 2048 // ~3ms @ 44100Hz

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

func (d *StreamDevice) encodeSamples(inputCh chan int32, outputCh chan byte) {
	var buf bytes.Buffer
	for sample := range inputCh {
		// glog.Infof("Got sample: %d", sample)
		for c := 0; c < d.config.Channels; c++ {
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
		}

		for _, b := range buf.Bytes() {
			outputCh <- b
		}
		buf.Reset()
	}
	glog.Infof("Done recieving samples, closing output channel")
	close(outputCh)
}

func (d *StreamDevice) writeByteCh(outputCh chan byte) {
	var buf bytes.Buffer
	var frameBytes int = d.config.PeriodSize * d.config.Channels * d.config.SampleSizeBytes()
	glog.Infof("Output frame size: %d bytes", frameBytes)

	frameCh := make(chan []byte, 5)
	go d.writeFrame(frameCh)

	// fillStart := time.Now()
	var more = true
	for more {
		b, more := <-outputCh
		// read enough bytes to fill buffer
		err := buf.WriteByte(b)
		if err != nil {
			d.doneCh <- err
		}

		// glog.Infof("Current output buffer size: %d", buf.Len())
		// flush buffer to device
		if buf.Len() >= frameBytes || !more {
			// outputFill := time.Since(fillStart)
			// glog.Infof("outputBufferFill Time %s", outputFill)
			// glog.Infof("Flushing output buffer")
			writeBuf := make([]byte, frameBytes)
			n, err := buf.Read(writeBuf)
			if err != nil {
				d.doneCh <- err
			}
			if n != frameBytes {
				glog.Infof("Writing non-standard frame: %d bytes", n)
			}

			// glog.Info("Flushing full frame")
			frameCh <- writeBuf

			// fillStart = time.Now()
			// glog.Infof("Buffer Length: %d", buf.Len())
		}
	}
	close(frameCh)
}

func (d *StreamDevice) writeFrame(frameCh chan []byte) {
	expectedFrameFlushDuration := time.Second * time.Duration(d.config.PeriodSize) / time.Duration(d.config.Rate)
	frameUnitSize := d.config.SampleSizeBytes() * d.config.Channels
	for frame := range frameCh {
		flushStart := time.Now()

		err := d.device.Write(frame, len(frame)/frameUnitSize)
		if err != nil {
			d.doneCh <- err
		}

		elapsed := time.Since(flushStart)
		if elapsed > expectedFrameFlushDuration {
			glog.Infof("Frame took too long to flush: %s, expected %s, size: %d", elapsed, expectedFrameFlushDuration, len(frame))
		}
	}
	close(d.doneCh)
}

func (d *StreamDevice) Stream() chan int32 {
	sampleCh := make(chan int32, 5)
	outputCh := make(chan byte, 5*d.config.SampleSizeBytes()*d.config.Channels)

	go d.encodeSamples(sampleCh, outputCh)
	go d.writeByteCh(outputCh)

	return sampleCh
}
