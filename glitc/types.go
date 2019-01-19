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
		fmtString = "%0.2d:%0.2d:%0.2d;%0.2d"
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

// dropFrame10MinIndex returns the number of frames since the beginning of this 10 minute drop frame window
func (f LTCFrame) dropFrame10MinIndex() int {
	m := f.Time.Minute()
	s := f.Time.Second()
	n := f.Time.Nanosecond()

	nanoseconds := int64((m%10*60+s))*1e9 + int64(n)
	framePeriod := f.FrameDuration().Nanoseconds()
	frameIndex := int(nanoseconds / int64(framePeriod))
	return frameIndex
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

	frameIndex := f.dropFrame10MinIndex()

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
	mTen := f.Time.Minute() / 10
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

// FrameIndex returns the number of whole frames from timecode 00:00:00:00
func (f LTCFrame) FrameIndex() int {
	if !f.DropFrame {
		return int(float64(f.Time.Hour()*3600+f.Time.Minute()*60+f.Time.Second())*f.EffectiveFPS() + float64(f.Frame().Frame))
	}

	return int(float64(f.Time.Hour())*3600*f.EffectiveFPS() +
		float64(f.Time.Minute()/10)*60*10*f.EffectiveFPS() +
		float64(f.dropFrame10MinIndex()))
}

// FrameBeginTime returns the time this frame starts
func (f LTCFrame) FrameBeginTime() time.Time {
	midnightLocal := time.Date(f.Time.Year(), f.Time.Month(), f.Time.Day(), 0, 0, 0, 0, f.Time.Location())
	return midnightLocal.Add(time.Duration(f.FrameIndex()) * f.FrameDuration())
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
