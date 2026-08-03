package main

import (
	"io"
	"testing"
	"time"

	"openscale/internal/fake"
	"openscale/internal/scale/serial"
)

// The serial port a capture is driven against: a scripted stream that hands back the
// bytes of a scale, read by read, ON THE INJECTED CLOCK. It is what makes the whole of
// `openscale capture` testable with no scale on the bench (§9.1).

// --- the port ---------------------------------------------------------------------

// scriptedRead is one answer of a scripted port: how long the read took, what it
// hands back, and whether it fails.
type scriptedRead struct {
	after time.Duration
	data  string
	err   error
}

// scriptedStream is the io.ReadCloser a test hands back instead of a serial port.
//
// Every read ADVANCES THE INJECTED CLOCK by the delay the script gives it, which is
// what lets a thirty-minute capture be exercised in microseconds and without a single
// time.Sleep: the instants the capture records are the ones the script decided, and
// the cadence it measures is the one the script emitted at.
type scriptedStream struct {
	clock  *fake.Clock
	script []scriptedRead
	at     int
	closes int
	// silence is what the stream does once the script runs dry: it comes back with no
	// byte and no error, which is what a real port does between two frames, and it is
	// what lets the capture reach its deadline.
	silence time.Duration
	// endErr, when set, is what the port answers instead of staying silent -- a cable
	// pulled in the middle of a measurement campaign.
	endErr error
	// link is the last set of options the opener was handed, and opens counts how many
	// times it was called at all.
	//
	// A double that IGNORED its options cannot tell a caller which built a usable link
	// from one which handed over a struct with no bitrate, no parity and no stop bits --
	// and a real port refuses the second before it touches the device. Recording them is
	// what makes that assertion possible; internal/scale/gramxfoc does the same with the
	// port name.
	link  serial.Options
	opens int
}

// newScriptedStream returns a port that answers these reads, in order, and then goes
// quiet one read timeout at a time.
func newScriptedStream(clock *fake.Clock, script ...scriptedRead) *scriptedStream {
	return &scriptedStream{clock: clock, script: script, silence: time.Second}
}

// emitting returns a port that sends count frames at the nominal cadence of the
// script, marking the frames at the given ranks unstable.
func emitting(clock *fake.Clock, count int, unstableRanks ...int) *scriptedStream {
	unstable := make(map[int]bool, len(unstableRanks))
	for _, rank := range unstableRanks {
		unstable[rank] = true
	}
	script := make([]scriptedRead, 0, count)
	for rank := 1; rank <= count; rank++ {
		frame := nominalFrame
		if unstable[rank] {
			frame = unstableFrame
		}
		script = append(script, scriptedRead{after: cadence, data: frame})
	}
	return newScriptedStream(clock, script...)
}

func (s *scriptedStream) Read(buffer []byte) (int, error) {
	if s.at >= len(s.script) {
		s.clock.Advance(s.silence)
		return 0, s.endErr
	}
	read := s.script[s.at]
	s.at++
	s.clock.Advance(read.after)
	return copy(buffer, read.data), read.err
}

func (s *scriptedStream) Close() error {
	s.closes++
	return nil
}

// opener is the seam capture is injected through: a serial port cannot be opened by
// `go test`, so the whole command is exercised through this.
//
// It KEEPS what it was handed, so that a test can assert on the link a caller built and
// not only on the bytes it read back.
func (s *scriptedStream) opener() serial.Opener {
	return func(o serial.Options) (io.ReadCloser, error) {
		s.link = o
		s.opens++
		return s, nil
	}
}

// refusingOpener is a port that is not there: the commonest failure of the bench, and
// the one a volunteer meets when the adapter is on another COM number.
func refusingOpener(err error) serial.Opener {
	return func(serial.Options) (io.ReadCloser, error) { return nil, err }
}

// Compile-time proof that the scripted port satisfies what an Opener has to return.
var _ io.ReadCloser = (*scriptedStream)(nil)

// TestScriptedStreamNeverNeedsTheRealClock guards the guard: if this double ever
// stopped driving the injected clock, every temporal assertion above would silently
// become a test of nothing.
func TestScriptedStreamNeverNeedsTheRealClock(t *testing.T) {
	clock := fake.NewClock(captureStart)
	stream := emitting(clock, 3)
	buffer := make([]byte, 64)
	for i := 1; i <= 3; i++ {
		n, err := stream.Read(buffer)
		if err != nil || n == 0 {
			t.Fatalf("lecture %d : %d octets, %v", i, n, err)
		}
		if want := captureStart.Add(time.Duration(i) * cadence); !clock.Now().Equal(want) {
			t.Fatalf("lecture %d : horloge à %s, %s attendu", i, clock.Now(), want)
		}
	}
}
