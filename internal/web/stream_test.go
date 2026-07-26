package web

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"
)

// liveStream is one SSE connection a test reads events out of.
type liveStream struct {
	response *http.Response
	reader   *bufio.Reader
	cancel   context.CancelFunc
}

// openStream connects to /api/v1/stream and returns it once the handler has ANSWERED.
//
// « Once it has answered » matters: the subscriber counter is incremented inside the
// handler, so a test that opened eight connections without reading anything would be
// asserting against a race rather than against the cap.
func (b *bench) openStream() (*liveStream, int) {
	b.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, b.http.URL+"/api/v1/stream", nil)
	if err != nil {
		cancel()
		b.t.Fatalf("requête de flux : %v", err)
	}
	response, err := b.client.Do(request)
	if err != nil {
		cancel()
		b.t.Fatalf("ouverture du flux : %v", err)
	}
	return &liveStream{response: response, reader: bufio.NewReader(response.Body), cancel: cancel},
		response.StatusCode
}

// next reads one SSE event and returns its name and its payload.
func (s *liveStream) next(t *testing.T) (string, string) {
	t.Helper()
	name, data := "", ""
	for {
		line, err := s.reader.ReadString('\n')
		if err != nil {
			t.Fatalf("lecture du flux : %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		case line == "" && (name != "" || data != ""):
			return name, data
		}
	}
}

// close ends the connection the way a browser tab does: abruptly.
func (s *liveStream) close() {
	s.cancel()
	_ = s.response.Body.Close()
}

// state reads the current state off a fresh stream, which is the first thing every
// subscriber receives.
func (b *bench) state() stateDTO {
	b.t.Helper()
	stream, status := b.openStream()
	defer stream.close()
	if status != http.StatusOK {
		b.t.Fatalf("GET /api/v1/stream = %d", status)
	}
	name, data := stream.next(b.t)
	if name != "state" {
		b.t.Fatalf("premier événement = %q, attendu « state »", name)
	}
	var out stateDTO
	if err := json.Unmarshal([]byte(data), &out); err != nil {
		b.t.Fatalf("événement illisible (%s) : %v", data, err)
	}
	return out
}

// TestTheFirstEventCarriesTheWholeState is the reason a browser that has just
// restarted needs no extra request: it is correct from its first byte.
func TestTheFirstEventCarriesTheWholeState(t *testing.T) {
	b := newBench(t)
	b.feed(1236, 2)

	got := b.state()
	if !got.Weight.Available || got.Weight.GrossG != 1236 {
		t.Fatalf("premier événement : poids %+v", got.Weight)
	}
	if got.Weight.NetText != "1,236" {
		t.Fatalf("poids affiché = %q, attendu « 1,236 »", got.Weight.NetText)
	}
}

// TestAnAbruptlyDisconnectedSubscriberLeavesNothingBehind is the test the mission
// asks for by name: neither a goroutine nor an entry.
//
// « Entry » is checked twice over, and neither check is redundant. The subscriber
// COUNTER coming back to zero says the handler returned; the fake clock coming back to
// its baseline of pending tickers says the heartbeat was stopped, which is the one
// resource a returning handler could still leak. And the goroutine count says that
// nothing is still parked on a channel nobody will ever write to.
func TestAnAbruptlyDisconnectedSubscriberLeavesNothingBehind(t *testing.T) {
	b := newBench(t)
	// One connection first, so that the baseline includes whatever the HTTP client and
	// the test server allocate for a connection and never give back.
	warmup, _ := b.openStream()
	warmup.next(t)
	warmup.close()
	settle(t, func() bool { return b.server.subscribers.Load() == 0 })

	baseline := runtime.NumGoroutine()
	_, tickersBefore := b.clock.Pending()

	for i := 0; i < 20; i++ {
		stream, status := b.openStream()
		if status != http.StatusOK {
			t.Fatalf("abonné %d refusé : %d", i, status)
		}
		stream.next(t)
		stream.close()
		settle(t, func() bool { return b.server.subscribers.Load() == 0 })
	}

	if got := b.server.subscribers.Load(); got != 0 {
		t.Fatalf("%d abonnés encore comptés après 20 déconnexions brutales", got)
	}
	if _, tickersAfter := b.clock.Pending(); tickersAfter != tickersBefore {
		t.Fatalf("%d tickers vivants, %d avant : un battement de cœur n'a pas été arrêté",
			tickersAfter, tickersBefore)
	}
	if got := converge(baseline); got > baseline+2 {
		t.Fatalf("%d goroutines après 20 déconnexions, %d avant : fuite", got, baseline)
	}
}

// TestTheNinthSubscriberIsRefusedCleanly is the second test the mission names.
//
// §13.1 budgets eight streams. The ninth gets 503 and a French sentence IMMEDIATELY:
// making it wait would hold an HTTP goroutine for as long as somebody keeps a tab
// open, in the very component whose goroutine inventory claims to be exhaustive.
func TestTheNinthSubscriberIsRefusedCleanly(t *testing.T) {
	b := newBench(t)

	live := make([]*liveStream, 0, maxSubscribers)
	for i := 0; i < maxSubscribers; i++ {
		stream, status := b.openStream()
		if status != http.StatusOK {
			t.Fatalf("abonné %d refusé alors que %d sont admis : %d", i+1, maxSubscribers, status)
		}
		// Reading the first event is what proves the handler ran and counted itself.
		stream.next(t)
		live = append(live, stream)
	}
	t.Cleanup(func() {
		for _, stream := range live {
			stream.close()
		}
	})

	refused, status := b.openStream()
	defer refused.close()
	if status != http.StatusServiceUnavailable {
		t.Fatalf("9ᵉ abonné : %d, attendu 503", status)
	}
	if got := refused.response.Header.Get("Retry-After"); got == "" {
		t.Error("le refus n'indique pas quand réessayer")
	}
	// It answered, and it answered NOW: a body that never comes is the failure this
	// test exists to forbid.
	raw := make([]byte, 512)
	n, _ := refused.response.Body.Read(raw)
	if !strings.Contains(string(raw[:n]), "maximum") {
		t.Fatalf("le refus ne dit pas pourquoi : %q", raw[:n])
	}

	// And a slot freed is a slot reusable: the cap is a live count, not a high-water
	// mark that would lock a station out after eight page reloads.
	live[0].close()
	live = live[1:]
	settle(t, func() bool { return b.server.subscribers.Load() == int64(maxSubscribers-1) })
	replacement, status := b.openStream()
	defer replacement.close()
	if status != http.StatusOK {
		t.Fatalf("après une libération, le nouvel abonné est refusé : %d", status)
	}
	replacement.next(t)
}

// TestAStreamEndsWhenTheHubCloses is the second half of §13.4: the handler leaves on a
// closed channel, which is what keeps Shutdown from burning its whole budget on a
// stream that is active by nature.
func TestAStreamEndsWhenTheHubCloses(t *testing.T) {
	b := newBench(t)
	stream, status := b.openStream()
	defer stream.close()
	if status != http.StatusOK {
		t.Fatalf("GET /api/v1/stream = %d", status)
	}
	stream.next(t)

	b.station.Stop()

	// The body ends rather than hanging: that is the whole assertion, and it is what
	// the browser sees as « the service went away », after which EventSource reconnects
	// on its own.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = stream.reader.ReadString('\n')
		for {
			if _, err := stream.reader.ReadString('\n'); err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(hang):
		t.Fatal("le flux SSE ne s'est pas terminé après l'arrêt du Hub")
	}
}

// settle yields until a condition holds, or fails the test.
//
// It yields rather than sleeping: a handler that returned has not necessarily been
// scheduled, and what a test can honestly assert is that the count converges.
func settle(t *testing.T, holds func() bool) {
	t.Helper()
	for i := 0; i < 200000; i++ {
		if holds() {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("la condition attendue ne s'est jamais réalisée")
}

// converge reads the goroutine count once it has stopped moving.
func converge(want int) int {
	got := runtime.NumGoroutine()
	for i := 0; i < 200000 && got > want; i++ {
		runtime.Gosched()
		got = runtime.NumGoroutine()
	}
	return got
}
