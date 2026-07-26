package kiosk

import (
	"testing"
	"time"
)

// start is the instant the crash tests count from. Any instant does: the rule is about
// intervals, and nothing in it reads the real clock.
var start = time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)

// TestTwentyQuickDeathsAreToleratedAndTheTwentyFirstIsNot is the rule of §15.2 at its
// boundary, in both directions.
//
// The boundary is the whole value of the number: at twenty the station is still trying,
// because a transient — a graphics driver waking up, a profile being wiped — deserves
// twenty attempts; at twenty-one it stops flickering in front of the queue.
func TestTwentyQuickDeathsAreToleratedAndTheTwentyFirstIsNot(t *testing.T) {
	var counter CrashCounter
	for death := 1; death <= CrashLimit; death++ {
		at := start.Add(time.Duration(death) * time.Second)
		if counter.Record(at, ShortLife-time.Millisecond) {
			t.Fatalf("page de secours dès la mort n° %d, la règle en tolère %d", death, CrashLimit)
		}
	}
	if !counter.Record(start.Add(21*time.Second), ShortLife-time.Millisecond) {
		t.Fatalf("mort n° %d : la page de secours doit s'ouvrir", CrashLimit+1)
	}
}

// TestABrowserThatLivedIsNotACrash is what keeps a station that somebody closed with
// Alt+F4 twenty-one times from ending on the rescue page: each of those deaths followed
// a browser that DID show the client screen.
func TestABrowserThatLivedIsNotACrash(t *testing.T) {
	var counter CrashCounter
	for death := 1; death <= 30; death++ {
		at := start.Add(time.Duration(death) * time.Minute)
		if counter.Record(at, ShortLife) {
			t.Fatalf("page de secours après %d fermetures ordinaires", death)
		}
	}
	if counter.ShortLives() != 0 {
		t.Fatalf("%d morts rapides comptées alors qu'aucune ne l'était", counter.ShortLives())
	}
}

// TestOneLongLifeResetsTheCount is the sentence of the rule that makes it about NOW:
// twenty crashes then a browser that stayed up means the station recovered, and the next
// crash must start counting from zero.
func TestOneLongLifeResetsTheCount(t *testing.T) {
	var counter CrashCounter
	for death := 1; death <= CrashLimit; death++ {
		counter.Record(start.Add(time.Duration(death)*time.Second), time.Second)
	}
	if counter.Record(start.Add(time.Minute), 20*time.Minute) {
		t.Fatal("un navigateur resté 20 minutes ne doit pas ouvrir la page de secours")
	}
	if counter.ShortLives() != 0 {
		t.Fatalf("le compte n'est pas remis à zéro : %d", counter.ShortLives())
	}
	if counter.Record(start.Add(2*time.Minute), time.Second) {
		t.Fatal("la première mort après une vie longue ne peut pas être la vingt-et-unième")
	}
}

// TestDeathsLeaveTheWindowAfterAnHour is the « dans l'heure » of §15.2: a station that
// crashed twenty times last Tuesday must not be in rescue mode today.
func TestDeathsLeaveTheWindowAfterAnHour(t *testing.T) {
	var counter CrashCounter
	for death := 1; death <= CrashLimit; death++ {
		counter.Record(start.Add(time.Duration(death)*time.Second), time.Second)
	}
	// One hour and a bit later: the twenty deaths above are outside the window, so this
	// one is the first of a new one.
	if counter.Record(start.Add(CrashWindow+time.Minute), time.Second) {
		t.Fatal("des morts vieilles de plus d'une heure comptent encore")
	}
	if counter.ShortLives() != 1 {
		t.Fatalf("%d morts dans la fenêtre, attendu 1", counter.ShortLives())
	}
}
