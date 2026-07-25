package domain

import (
	"errors"
	"fmt"
	"strconv"
	"testing"
)

// TestParseCentsAcceptsWhatTheRealFilesContain: the values are taken from
// testdata/catalog/flv.csv, where 330 rows carry two decimals and 25 carry one.
func TestParseCentsAcceptsWhatTheRealFilesContain(t *testing.T) {
	cases := []struct {
		in   string
		want Cents
	}{
		{"16.05", 1605}, // real row of flv.csv
		{"4.3", 430},    // one decimal, real row: 4,30 EUR and NOT 4,03
		{"7.89", 789},   // LENTILLES VERTES, line 2
		{"4.57", 457},   // POIS CHICHES -VRAC, line 3
		{"3", 300},      // no decimal at all
		{"0.05", 5},
		{"0", 0},
		{"5,32", 532}, // the comma tolerance: the garlic of the document
		{"5,3", 530},
		{"9999.99", 999999}, // MaxUnitPrice exactly
	}
	for _, c := range cases {
		got, err := ParseCents(c.in)
		if err != nil {
			t.Errorf("ParseCents(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseCents(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseCentsRefusesWhatItMustNotGuess(t *testing.T) {
	for _, in := range []string{
		"", ".", ",", ".5", ",5", "-1", "+1", "1 ", " 1", "1.2.3", "1,2,3",
		"abc", "1a", "1.2a", "5.321", "1.234", "10000.00", "1e3", "0x10",
	} {
		if got, err := ParseCents(in); !errors.Is(err, ErrPriceFormat) {
			t.Errorf("ParseCents(%q) = %d, %v; want ErrPriceFormat", in, got, err)
		}
	}
}

// TestParseCentsNeverAgreesWithAFloatOnTheHardCases is the demonstration made
// executable: the integer parser is right where the float is wrong.
func TestParseCentsNeverAgreesWithAFloatOnTheHardCases(t *testing.T) {
	// Prices whose float64 product with 100 truncates one cent too low.
	broken := 0
	for cents := int64(1); cents <= 99_999; cents++ {
		decimal := fmt.Sprintf("%d.%02d", cents/100, cents%100)
		asFloat, err := strconv.ParseFloat(decimal, 64)
		if err != nil {
			t.Fatalf("ParseFloat(%q): %v", decimal, err)
		}
		if int64(asFloat*100) != cents {
			broken++
			// Whatever the float does, the integer parser must be exact.
			got, err := ParseCents(decimal)
			if err != nil {
				t.Fatalf("ParseCents(%q): %v", decimal, err)
			}
			if int64(got) != cents {
				t.Fatalf("ParseCents(%q) = %d, want %d", decimal, got, cents)
			}
		}
	}
	if broken == 0 {
		t.Skip("no float64 truncation on this platform; the parser is exact anyway")
	}
	t.Logf("float64 truncation would have lost %d of the 99 999 prices; ParseCents lost none", broken)
}

func TestEuroFormatsForTheLabel(t *testing.T) {
	cases := []struct {
		in   Cents
		want string
	}{
		{532, "5,32"}, // the printed price per kilo of the garlic
		{658, "6,58"}, // solidarity amount
		{592, "5,92"}, // member amount
		{479, "4,79"}, // member unit price
		{5, "0,05"},
		{0, "0,00"},
		{100, "1,00"},
		{999999, "9999,99"},
		{-282, "-2,82"}, // the basket-missing case reaches the diagnostic screen
	}
	for _, c := range cases {
		if got := c.in.Euro(); got != c.want {
			t.Errorf("Cents(%d).Euro() = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestKilosFormatsForTheLabel(t *testing.T) {
	cases := []struct {
		in   Grams
		want string
	}{
		{1236, "1,236"}, // the reference weight
		{850, "0,850"},
		{5, "0,005"},
		{0, "0,000"},
		{99999, "99,999"},
		{12345, "12,345"},
		{-282, "-0,282"},
	}
	for _, c := range cases {
		if got := c.in.Kilos(); got != c.want {
			t.Errorf("Grams(%d).Kilos() = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestParseCentsAndEuroRoundTrip: what we read we can print, and back.
func TestParseCentsAndEuroRoundTrip(t *testing.T) {
	for cents := Cents(0); cents <= MaxUnitPrice; cents += 7 {
		printed := cents.Euro()
		back, err := ParseCents(printed)
		if err != nil {
			t.Fatalf("ParseCents(%q): %v", printed, err)
		}
		if back != cents {
			t.Fatalf("%d -> %q -> %d", cents, printed, back)
		}
	}
}
