package main

import (
	"math"
	"testing"
	"time"
)

func TestDurationStatistics(t *testing.T) {
	s := DurationStatistics{}
	s.Update(1 * time.Second)
	t.Logf("%s", s)
	s.Update(2 * time.Second)
	t.Logf("%s", s)

	if s.average != 1500*time.Millisecond {
		t.Errorf("Incorrect average, expected 1.75s, got %s", s.average)
	}

	if s.StdDev() != 707106781*time.Nanosecond {
		t.Errorf("Wrong stddev, expected 250ms got %s", s.StdDev())
	}
}

func TestSlow(t *testing.T) {
	s := DurationStatistics{average: time.Millisecond}.Slow(1501 * time.Microsecond)
	if !s {
		t.Errorf("Expected slow frame, returned false")
	}
}

func TestTimeRing(t *testing.T) {
	r := NewTimeRing(10)
	r.Mark()
	f := r.First()
	l := r.Latest()
	if f != l {
		t.Errorf("Only one value in ring, first should equal last, %s != %s", f, l)
	}

	rate := r.AvgRate()
	if !math.IsInf(rate, 1) {
		t.Errorf("Got wrong rate: %f", rate)
	}
}
