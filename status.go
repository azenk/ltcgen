package main

import (
	"container/ring"
	"fmt"
	"math"
	"time"
)

type DurationStatistics struct {
	n        int
	average  time.Duration
	variance int64
	minMax   MinMaxDuration
}

func (s *DurationStatistics) Update(d time.Duration) {
	s.n++
	oldAvg := s.average
	s.average = oldAvg + (d-oldAvg)/time.Duration(s.n)
	s.variance += (d - s.average).Nanoseconds() * (d - oldAvg).Nanoseconds()
	s.minMax.Update(d)
}

func (s DurationStatistics) Variance() time.Duration {
	if s.n > 1 {
		return time.Duration(s.variance / int64(s.n-1))
	}
	return 0
}

func (s DurationStatistics) Slow(d time.Duration) bool {
	return d > s.average+100*time.Microsecond
}

func (s DurationStatistics) StdDev() time.Duration {
	return time.Duration(math.Sqrt(float64(s.Variance().Nanoseconds())))
}

func (s DurationStatistics) String() string {
	return fmt.Sprintf("(min/mean/stddev/max): %s/%s/%s/%s", s.minMax.Min(), s.average, s.StdDev(), s.minMax.Max())
}

type MinMaxDuration struct {
	min     time.Duration
	max     time.Duration
	current time.Duration
}

func (m *MinMaxDuration) Update(d time.Duration) {
	if d < m.min || m.min == 0 {
		m.min = d
	} else if d > m.max {
		m.max = d
	}

	m.current = d
}

func (m MinMaxDuration) Min() time.Duration {
	return m.min
}

func (m MinMaxDuration) Max() time.Duration {
	return m.max
}

func (m MinMaxDuration) String() string {
	return fmt.Sprintf("(min/current/max): %s/%s/%s", m.min, m.current, m.max)
}

type TimeRing struct {
	*ring.Ring
	marked int
}

func NewTimeRing(len int) *TimeRing {
	r := &TimeRing{}
	r.Ring = ring.New(len)
	return r
}

func (r *TimeRing) Mark() {
	r.Ring = r.Next()
	r.marked = int(math.Min(float64(r.marked+1), float64(r.Ring.Len())))
	r.Ring.Value = time.Now()
}

func (r TimeRing) Latest() time.Time {
	return r.Value.(time.Time)
}

func (r TimeRing) First() time.Time {
	val, _ := r.Move(r.Len() - r.marked).Next().Value.(time.Time)
	return val
}

func (r *TimeRing) AvgRate() float64 {
	elapsed := r.Latest().Sub(r.First())
	return float64(r.marked) / elapsed.Seconds()
}

type Status struct {
	sent        int64
	dropped     int64
	duplicate   int64
	largeOffset int64
	start       time.Time
	lastSent    time.Time
	times       *TimeRing
	offset      DurationStatistics
}

func NewStatus(rateLen int) *Status {
	s := &Status{}
	s.times = NewTimeRing(rateLen)
	return s
}

func (s *Status) Sent(offset time.Duration) {
	s.times.Mark()
	s.sent++
	s.offset.Update(offset)
	if offset > time.Millisecond {
		s.largeOffset++
	}
}

func (s *Status) Dropped(number int) {
	s.dropped += int64(number)
}

func (s *Status) Duplicate() {
	s.duplicate++
}

func (s Status) FPS() float64 {
	return s.times.AvgRate()
}

func (s Status) String() string {
	pct := 100 * (1 - float64(s.largeOffset+s.dropped+s.duplicate)/float64(s.sent))
	return fmt.Sprintf("%d frames sent - %0.2f%% perfect %d/%d/%d drop/dup/slow - frame start offset %s", s.sent, pct, s.dropped, s.duplicate, s.largeOffset, s.offset)
}
