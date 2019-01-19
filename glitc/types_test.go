package glitc

import (
	"testing"
	"time"

	"github.com/go-test/deep"
)

func TestAsBCD(t *testing.T) {
	testCases := []struct {
		Name         string
		Number       int
		ExpectedTens int
		ExpectedOnes int
	}{
		{"OnesOnly", 7, 0, 7},
		{"TensOnes", 31, 3, 1},
		{"ExcessDigits", 131, 3, 1},
	}

	for _, c := range testCases {
		t.Run(c.Name, func(st *testing.T) {
			tens, ones := asBCD(c.Number)

			if tens != c.ExpectedTens {
				st.Errorf("Incorrect value for tens: got '%d' expected '%d'", tens, c.ExpectedTens)
			}
			if ones != c.ExpectedOnes {
				st.Errorf("Incorrect value for ones: got '%d' expected '%d'", tens, c.ExpectedOnes)
			}
		})
	}
}

func TestFrame(t *testing.T) {
	testCases := []struct {
		Name             string
		Frame            LTCFrame
		ExpectedTimeCode TimeCode
	}{
		{
			"25fps-0",
			LTCFrame{Time: time.Date(2018, 12, 1, 23, 0, 0, 0, time.Local), FramesPerSecond: 25},
			TimeCode{23, 0, 0, 0, false},
		},
		{
			"25fps-15",
			LTCFrame{Time: time.Date(2018, 12, 1, 23, 0, 0, 600000000, time.Local), FramesPerSecond: 25},
			TimeCode{23, 0, 0, 15, false},
		},
		{
			"29.97fps/df-29",
			LTCFrame{Time: time.Date(2018, 12, 1, 23, 1, 0, 999999999, time.Local), FramesPerSecond: 30, DropFrame: true},
			TimeCode{23, 1, 1, 0, true},
		},
		{
			"29.97fps/df-2",
			LTCFrame{Time: time.Date(2018, 12, 1, 23, 5, 0, 33366700, time.Local), FramesPerSecond: 30, DropFrame: true},
			TimeCode{23, 5, 0, 2, true},
		},
		{
			"29.97fps/df-3",
			LTCFrame{Time: time.Date(2018, 12, 1, 23, 2, 30, 0, time.Local), FramesPerSecond: 30, DropFrame: true},
			TimeCode{23, 2, 29, 29, true},
		},
		{
			"29.97fps/df-0",
			LTCFrame{Time: time.Date(2018, 12, 1, 23, 9, 59, 966633300, time.Local), FramesPerSecond: 30, DropFrame: true},
			TimeCode{23, 9, 59, 29, true},
		},
		{
			"29.97fps/df-8",
			LTCFrame{Time: time.Date(2018, 12, 1, 23, 8, 59, 966633300, time.Local), FramesPerSecond: 30, DropFrame: true},
			TimeCode{23, 8, 59, 28, true},
		},
		{
			"29.97fps/df-tens-0",
			LTCFrame{Time: time.Date(2018, 12, 1, 23, 10, 0, 0, time.Local), FramesPerSecond: 30, DropFrame: true},
			TimeCode{23, 10, 0, 0, true},
		},
	}

	for _, c := range testCases {
		t.Run(c.Name, func(st *testing.T) {
			tc := c.Frame.Frame()
			if diff := deep.Equal(tc, c.ExpectedTimeCode); len(diff) > 0 {
				st.Error("Encoded frame doesn't match expected value:")
				for _, l := range diff {
					st.Log(l)
				}
			}
		})
	}

}

func TestFrameDuration(t *testing.T) {
	testCases := []struct {
		Name                  string
		Frame                 LTCFrame
		ExpectedFrameDuration time.Duration
	}{
		{"30fps", LTCFrame{FramesPerSecond: 30}, 33333333 * time.Nanosecond},
		{"29.97fps(df)", LTCFrame{FramesPerSecond: 30, DropFrame: true}, 33366700 * time.Nanosecond},
		{"25fps", LTCFrame{FramesPerSecond: 25}, 40000000 * time.Nanosecond},
		{"24fps", LTCFrame{FramesPerSecond: 24}, 41666666 * time.Nanosecond},
	}

	for _, c := range testCases {
		t.Run(c.Name, func(st *testing.T) {
			frameDuration := c.Frame.FrameDuration()
			if frameDuration != c.ExpectedFrameDuration {
				st.Errorf("Incorrect value for frame number: got '%v' expected '%v'", frameDuration, c.ExpectedFrameDuration)
			}
		})
	}
}

func TestFrameEncode(t *testing.T) {
	testCases := []struct {
		Name          string
		Frame         LTCFrame
		ExpectedFrame []byte
	}{
		{
			"25fps-0",
			LTCFrame{Time: time.Date(2018, 12, 1, 23, 0, 0, 0, time.Local), FramesPerSecond: 25},
			[]byte{0x00, 0x10, 0x00, 0x00, 0x00, 0x00, 0xC0, 0x50, 0x3F, 0xFD},
		},
		{
			"30fps-0",
			LTCFrame{Time: time.Date(2018, 12, 1, 23, 14, 21, 0, time.Local), FramesPerSecond: 30},
			[]byte{0x00, 0x10, 0x80, 0x50, 0x20, 0x80, 0xC0, 0x40, 0x3F, 0xFD},
		},
		{
			"30fps/df-2",
			LTCFrame{Time: time.Date(2018, 12, 1, 23, 14, 0, 0, time.Local), FramesPerSecond: 30, DropFrame: true},
			[]byte{0x10, 0x70, 0x90, 0xB0, 0xC0, 0x80, 0xC0, 0x40, 0x3F, 0xFD},
		},
		{
			"30fps/df-0",
			LTCFrame{Time: time.Date(2018, 12, 1, 23, 40, 21, 0, time.Local), FramesPerSecond: 30, DropFrame: true, ExternalClockSync: true},
			[]byte{0x90, 0x70, 0x00, 0x40, 0x00, 0x20, 0xC0, 0x60, 0x3F, 0xFD},
		},
		{
			"30fps/df-0-userdata",
			LTCFrame{Time: time.Date(2018, 12, 1, 23, 40, 21, 0, time.Local), FramesPerSecond: 30, DropFrame: true, ExternalClockSync: true, UserBytes: &[4]byte{0xA5, 0xC3, 0x91, 0x72}},
			[]byte{0x95, 0x7A, 0x03, 0x4C, 0x01, 0x39, 0xC2, 0x67, 0x3F, 0xFD},
		},
		{
			"25fps-0-userdata",
			LTCFrame{Time: time.Date(2018, 12, 1, 23, 40, 21, 0, time.Local), FramesPerSecond: 25, ExternalClockSync: true, UserBytes: &[4]byte{0xA5, 0xC3, 0x91, 0x72}},
			[]byte{0x05, 0x1A, 0x83, 0x5C, 0x01, 0x29, 0xC2, 0x77, 0x3F, 0xFD},
		},
	}

	for _, c := range testCases {
		t.Run(c.Name, func(st *testing.T) {
			frameBytes := c.Frame.EncodeFrame()
			if diff := deep.Equal(frameBytes, c.ExpectedFrame); len(diff) > 0 {
				st.Error("Encoded frame doesn't match expected value:")
				st.Logf("Got time: %s", c.Frame.Frame())
				for _, l := range diff {
					st.Log(l)
				}
			}
		})
	}

}

func TestFrameBeginTime(t *testing.T) {
	zoneUSCentral, err := time.LoadLocation("US/Central")
	if err != nil {
		t.Fatalf("Unable to load US/Central Timezone: %v", err)
	}

	cases := []struct {
		Name              string
		Frame             LTCFrame
		ExpectedBeginTime time.Time
	}{
		{
			Name:              "midnight",
			Frame:             LTCFrame{Time: time.Date(2019, 1, 1, 0, 0, 0, 0, zoneUSCentral), FramesPerSecond: 30},
			ExpectedBeginTime: time.Date(2019, 1, 1, 0, 0, 0, 0, zoneUSCentral),
		},
	}

	for _, c := range cases {
		t.Run(c.Name, func(st *testing.T) {
			st.Logf("Frame Time: %s", c.Frame.Time)
			if begin := c.Frame.FrameBeginTime(); begin != c.ExpectedBeginTime {
				st.Errorf("begin time doesn't match expected: got %s, expected %s", begin, c.ExpectedBeginTime)
			}
		})
	}
}
