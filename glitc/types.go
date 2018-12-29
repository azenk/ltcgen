package glitc

import (
	"fmt"
	"math"
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

type TimeCode struct {
	Hour      int
	Minute    int
	Second    int
	Frame     int
	DropFrame bool
}

func (tc TimeCode) String() string {
	fmtString := "%0.2d:%0.2d:%0.2d:%0.2d"
	if tc.DropFrame {
		fmtString = "%0.2d;%0.2d;%0.2d;%0.2d"
	}
	return fmt.Sprintf(fmtString, tc.Hour, tc.Minute, tc.Second, tc.Frame)
}

type LTCFrame struct {
	Time              time.Time
	FramesPerSecond   float64
	DropFrame         bool
	ColorFrame        bool
	ExternalClockSync bool
	UserBytes         *[4]byte
}

// Frame returns current frame number
func (f LTCFrame) Frame() TimeCode {
	if !f.DropFrame {
		return TimeCode{
			Hour:      f.Time.Hour(),
			Minute:    f.Time.Minute(),
			Second:    f.Time.Second(),
			Frame:     int(float64(f.Time.Nanosecond()) / 1e9 * f.EffectiveFPS()),
			DropFrame: false,
		}
	}

	m := f.Time.Minute()
	mTen := f.Time.Minute() / 10
	s := f.Time.Second()
	n := f.Time.Nanosecond()

	nanoseconds := int64((m%10*60+s))*1e9 + int64(n)
	framePeriod := f.FrameDuration().Nanoseconds()
	frameIndex := int(nanoseconds / int64(framePeriod))

	var minute, second, frame int
	if frameIndex < 30*60 {
		minute = 0
	} else {
		minute = 1 + (frameIndex-30*60)/(30*59+28)
	}
	second = (frameIndex + 2*minute - 30*60*minute) / 30

	if minute == 0 {
		frame = frameIndex - second*30
	} else if minute != 0 && second == 0 {
		frame = 2 + (frameIndex - 1800 - (minute-1)*1798 - second*30)
	} else {
		frame = frameIndex + 2*minute - minute*30*60 - second*30
	}
	return TimeCode{
		Hour:      f.Time.Hour(),
		Minute:    mTen*10 + minute,
		Second:    second,
		Frame:     frame,
		DropFrame: true,
	}
}

// FrameDuration total frame duration
func (f LTCFrame) FrameDuration() time.Duration {
	return time.Second * 1000 / time.Duration(f.EffectiveFPS()*1000)
}

// BitPeriod the clock period used for encoding
func (f LTCFrame) BitPeriod() time.Duration {
	return f.FrameDuration() / 80
}

// EffectiveFPS returns effective frames per second
func (f LTCFrame) EffectiveFPS() float64 {
	if !f.DropFrame {
		return float64(f.FramesPerSecond)
	}
	return float64(30) * float64(18000.0-18.0) / float64(18000.0)
}

// EncodeFrame returns a byte array representing this LTCFrame
func (f LTCFrame) EncodeFrame() []byte {
	var externalClock, b10, b11, b27, b43, b59 int

	tc := f.Frame()

	hTens, hOnes := asBCD(tc.Hour)
	mTens, mOnes := asBCD(tc.Minute)
	sTens, sOnes := asBCD(tc.Second)
	fTens, fOnes := asBCD(tc.Frame)

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

	binaryFrame[7] |= byte(bits.Reverse8(uint8(hTens&0x3)) | uint8(externalClock&0x1)<<5 | uint8(b59&0x1)<<4)
	binaryFrame[6] |= byte(bits.Reverse8(uint8(hOnes & 0xF)))

	binaryFrame[5] |= byte(bits.Reverse8(uint8(mTens&0x7)) | uint8(b43&0x1)<<4)
	binaryFrame[4] |= byte(bits.Reverse8(uint8(mOnes & 0xF)))

	binaryFrame[3] |= byte(bits.Reverse8(uint8(sTens&0x7)) | uint8(b27&0x1)<<4)
	binaryFrame[2] |= byte(bits.Reverse8(uint8(sOnes & 0xF)))

	binaryFrame[1] |= byte(bits.Reverse8(uint8(fTens&0x3)) | uint8(b10&0x1)<<5 | uint8(b11&0x1)<<4)
	binaryFrame[0] |= byte(bits.Reverse8(uint8(fOnes & 0xF)))

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
	sampleCount := int(float64(sampleRate) / f.EffectiveFPS())

	samples := make([]int32, sampleCount)
	samplesPerBit := int(float64(sampleRate) / (f.EffectiveFPS() * 80))
	clockErr := int(math.Round(float64(sampleRate*10)/(f.EffectiveFPS()*80))) % 10

	binaryFrame := f.EncodeFrame()

	currentValue := -1 * amplitude
	var sample int
	for bit := 0; bit < 80; bit++ {
		var c1, c2 int
		c1 = samplesPerBit >> 1
		c2 = samplesPerBit >> 1

		if samplesPerBit%2 == 1 {
			c2 = c2 + 1
		}

		if bit%10 < clockErr {
			c1 = c1 + 1
		}

		if c1+c2 > sampleCount-sample {
			// glog.Infof("Frame would be too long by %d samples, trimming", c1+c2-(sampleCount-sample))
			c2 = sampleCount - sample - c1
		}

		// glog.Infof("Bit: %d, %d/%d\n", bit, c1, c2)
		byteOffset := bit / 8
		bitValue := (binaryFrame[byteOffset] >> uint(7-bit%8)) & 0x1

		// always transition on positive clock edge
		if currentValue > 0 {
			currentValue = -1 * amplitude
		} else {
			currentValue = amplitude
		}

		for i := 0; i < c1; i++ {
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

		for i := 0; i < c2; i++ {
			samples[sample] = int32(currentValue)
			sample++
		}
	}
	// glog.Infof("Samples per bit: %d, TotalSamples: %d/%d", samplesPerBit, sample, sampleCount)

	// any remaining frames are set to minimum
	for ; sample < len(samples); sample++ {
		samples[sample] = int32(currentValue)
	}

	return samples
}
