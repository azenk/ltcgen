package glitc

import (
	"fmt"
	"math/bits"
	"time"
)

// SyncBits The sync pattern in bits 64 through 79 includes 12 consecutive 1 bits,
// which cannot appear anywhere else in the time code.  Assuming all user bits are
// set to 1, the longest run of 1 bits that can appear elsewhere in the time code
// is 10, bits 9 to 18 inclusive. The sync pattern is preceded by 00 and followed
// by 01. This is used to determine whether an audio tape is running forward or backward.
const SyncBits = 0x3FFD

func asBCD(number int) (int, int) {
	var ones, tens int
	ones = number % 10
	tens = (number - ones) / 10 % 10
	return tens, ones
}

type LTCFrame struct {
	Time              time.Time
	FramesPerSecond   int
	DropFrame         bool
	ColorFrame        bool
	ExternalClockSync bool
	UserBytes         *[4]byte
}

func (f LTCFrame) String() string {
	return fmt.Sprintf("%0.2d:%0.2d:%0.2d:%0.2d", f.Time.Hour(), f.Time.Minute(), f.Time.Second(), f.Frame())
}

// Frame returns current frame number
func (f LTCFrame) Frame() int {
	rawFrameNumber := float64(f.Time.Nanosecond()) / 1e9 * float64(f.FramesPerSecond)
	if rawFrameNumber <= 1 && f.DropFrame && f.Time.Second() == 0 && f.Time.Minute()%10 != 0 {
		rawFrameNumber = 2
	}
	return int(rawFrameNumber)
}

// FrameDuration total frame duration
func (f LTCFrame) FrameDuration() time.Duration {
	return time.Second / time.Duration(f.FramesPerSecond)
}

// BitPeriod the clock period used for encoding
func (f LTCFrame) BitPeriod() time.Duration {
	return f.FrameDuration() / 80
}

// EncodeFrame returns a byte array representing this LTCFrame
func (f LTCFrame) EncodeFrame() []byte {
	var externalClock, b10, b11, b27, b43, b59 int

	hTens, hOnes := asBCD(f.Time.Hour())
	mTens, mOnes := asBCD(f.Time.Minute())
	sTens, sOnes := asBCD(f.Time.Second())
	fTens, fOnes := asBCD(f.Frame())

	if f.DropFrame {
		b10 = 1
	}

	// set color bit to 1
	b11 = 1

	if f.ExternalClockSync {
		externalClock = 1
	}

	binaryFrame := make([]byte, 10)

	if f.UserBytes != nil {
		if f.FramesPerSecond == 25 {
			b27 = 1
		} else {
			b43 = 1
		}
		binaryFrame[7] |= f.UserBytes[3] >> 4 & 0xF
		binaryFrame[6] |= f.UserBytes[3] & 0xF
		binaryFrame[5] |= f.UserBytes[2] >> 4 & 0xF
		binaryFrame[4] |= f.UserBytes[2] & 0xF
		binaryFrame[3] |= f.UserBytes[1] >> 4 & 0xF
		binaryFrame[2] |= f.UserBytes[1] & 0xF
		binaryFrame[1] |= f.UserBytes[0] >> 4 & 0xF
		binaryFrame[0] |= f.UserBytes[0] & 0xF
	}

	binaryFrame[9] |= SyncBits & 0xFF
	binaryFrame[8] |= SyncBits >> 8 & 0xFF

	binaryFrame[7] |= byte((hTens&0x3)<<6 | (externalClock&0x1)<<5 | (b59&0x1)<<4)
	binaryFrame[6] |= byte((hOnes & 0xF) << 4)

	binaryFrame[5] |= byte((mTens&0x7)<<5 | (b43&0x1)<<4)
	binaryFrame[4] |= byte((mOnes & 0xF) << 4)

	binaryFrame[3] |= byte((sTens&0x7)<<5 | (b27&0x1)<<4)
	binaryFrame[2] |= byte((sOnes & 0xF) << 4)

	binaryFrame[1] |= byte((fTens&0x3)<<6 | (b10&0x1)<<5 | (b11&0x1)<<4)
	binaryFrame[0] |= byte((fOnes & 0xF) << 4)

	var ones int
	for _, b := range binaryFrame {
		ones += bits.OnesCount8(uint8(b))
	}

	if ones%2 != 0 {
		if f.FramesPerSecond == 25 {
			binaryFrame[7] |= 0x10
		} else {
			binaryFrame[3] |= 0x10
		}
	}

	return binaryFrame
}

func (f LTCFrame) GetAudioSamples(sampleRate int, amplitude int) []int32 {
	sampleCount := sampleRate / f.FramesPerSecond

	samples := make([]int32, sampleCount)
	samplesPerBit := len(samples) / 80

	binaryFrame := f.EncodeFrame()

	currentValue := -1 * amplitude
	var sample int
	for bit := 0; bit < 80; bit++ {
		byteOffset := bit / 8
		bitValue := (binaryFrame[byteOffset] >> uint(bit%8)) & 0x1

		// always transition on positive clock edge
		if currentValue > 0 {
			currentValue = -1 * amplitude
		} else {
			currentValue = amplitude
		}

		for i := 0; i < samplesPerBit/2; i++ {
			samples[sample] = int32(currentValue)
			sample++
		}

		// transition again on ones
		if bitValue != 0 {
			if currentValue > 0 {
				currentValue = -1 * amplitude
			} else {
				currentValue = amplitude
			}
		}

		for i := 0; i < samplesPerBit/2; i++ {
			samples[sample] = int32(currentValue)
			sample++
		}
	}

	// any remaining frames are set to minimum
	for ; sample < len(samples); sample++ {
		samples[sample] = int32(currentValue)
	}

	return samples
}
