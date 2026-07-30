package raster

import (
	"fmt"

	"openscale/internal/domain"
)

// The bounds of the manual, each one carrying the command that imposes it (§8.3).
//
// They are not style choices and none of them is negotiable from a screen: a value
// outside them produces a frame the firmware silently ignores, and the only symptom on
// site is « l'imprimante n'imprime rien ».
const (
	// MinDarkness and MaxDarkness bound <#E>. Five steps of heat, and the right one
	// depends on the roll: a paper that takes 3 well goes grey at 2 and bleeds at 5.
	MinDarkness = 1
	MaxDarkness = 5

	// MinSpeed and MaxSpeed bound <CS>, in inches per second. Slower is darker and
	// sharper; faster is a queue that moves. It is set on a real run, like the rest.
	MinSpeed = 2
	MaxSpeed = 6

	// MaxOffsetDots is the width of the <A3> fields: four digits, so ±9999 dots. It is
	// the bound of the LANGUAGE. The bound that really matters is geometric and is
	// checked at encoding time, against the ink of the bitmap about to be sent.
	MaxOffsetDots = 9999

	// MaxCopies is the width of the <Q> field: six digits.
	//
	// It is the bound of the language and NOT a shop policy, and it bounds what ONE JOB
	// may ask for. How many labels a self-service station should ever print in one go is
	// a business question the manual does not answer; putting an invented ceiling here
	// would look like a measured one.
	MaxCopies = 999_999

	// MinConfiguredCopies and MaxConfiguredCopies bound printer.options.copies, which is
	// the count a job that names none falls back on.
	//
	// TEN, and unlike every bound above it that figure is a STATION POLICY rather than a
	// measurement — said out loud here so that nobody reads it as one. It is the ceiling
	// the administration form already offered and the one Config.Validate already
	// applied, and both disagreed with this file: the schema refused eleven copies while
	// Settings.Validate accepted five hundred, so the same number got two answers
	// depending on whether it was typed in the screen or in the file.
	//
	// The convergence goes towards TEN and not towards the six digits of <Q>: a customer
	// sticks ONE label on ONE bag, `"copies": 100` is a typing accident, and the accident
	// costs a roll and a queue at the scale. What the wire accepts is the other question,
	// and MaxCopies goes on answering it.
	MinConfiguredCopies = 1
	MaxConfiguredCopies = 10
)

// Settings are the adjustments a volunteer makes ON A REAL PRINT RUN, plus the two
// values the frame cannot be built without.
//
// The three of §8.2 are Darkness, Speed and the offset. They are set with a printed
// label in hand — held over a current one on a light table for the offset, compared
// against a known-good label for the heat — never from a formula, which is exactly why
// they live in printer.options (§11.2) and not in this package.
//
// The ZERO VALUE IS NOT A CONFIGURATION. A darkness of zero is not a shade of grey, it
// is a field nobody filled, and New refuses it rather than quietly substituting a
// default: the file the station runs on then says what the printer was told, in full.
// DefaultSettings carries the values shipped in config-lacagette.json.
type Settings struct {
	// Darkness is the heat of the head, <#E>, from MinDarkness to MaxDarkness.
	Darkness int
	// Speed is the print speed in inches per second, <CS>, from MinSpeed to MaxSpeed.
	Speed int
	// OffsetXDots and OffsetYDots shift the WHOLE label on the media, <A3>. This is
	// the ±1 dot adjustment of the administration screen: the arrows move one dot at a
	// time, because that is the size of the correction a misplaced roll needs.
	OffsetXDots int
	OffsetYDots int
	// InvertBits flips the polarity of the <G> block.
	//
	// It is the LAST SBPL unknown, against seven before A2 (§8.3), and it is lifted in
	// ten minutes by the `alignment` self-test: print it, look at the square. Black
	// square on white label means the polarity shipped here is right; a white square in
	// a black rectangle means this flag has to go up.
	InvertBits bool
	// Copies is what a job that names no count gets. One, on a scale where a customer
	// sticks one label on one bag.
	Copies int
}

// DefaultSettings returns the values shipped in the configuration of §11.2.
//
// They come from the file, not from an opinion held here, and they are a STARTING
// POINT for the adjustment on a real run — not a factory setting anybody should hope
// to keep as is on a roll they have not tested.
func DefaultSettings() Settings {
	return Settings{Darkness: 3, Speed: 4, OffsetXDots: 0, OffsetYDots: 0, Copies: 1}
}

// Validate reports every setting the manual would refuse, ALL AT ONCE.
//
// All at once because a volunteer who came to fix one file should leave having fixed
// it, and not discover the second fault after a restart (§11.3). Each fault names the
// configuration key it belongs to, so the message can be shown next to the field that
// carries it.
func (s Settings) Validate() []domain.Fault {
	var faults []domain.Fault
	fail := func(field, format string, args ...any) {
		faults = append(faults, domain.Fault{Field: field, Message: fmt.Sprintf(format, args...)})
	}

	if s.Darkness < MinDarkness || s.Darkness > MaxDarkness {
		fail("printer.options.darkness",
			"noircissement %d : la valeur va de %d à %d (commande SBPL <#E>). "+
				"Ce réglage se fait sur un tirage réel, pas au jugé",
			s.Darkness, MinDarkness, MaxDarkness)
	}
	if s.Speed < MinSpeed || s.Speed > MaxSpeed {
		fail("printer.options.speed",
			"vitesse %d : la valeur va de %d à %d pouces par seconde (commande SBPL <CS>)",
			s.Speed, MinSpeed, MaxSpeed)
	}
	if s.OffsetXDots < -MaxOffsetDots || s.OffsetXDots > MaxOffsetDots {
		fail("printer.options.offset_x",
			"décalage horizontal de %d dots : la commande SBPL <A3> porte quatre chiffres, "+
				"soit -%d à %d dots", s.OffsetXDots, MaxOffsetDots, MaxOffsetDots)
	}
	if s.OffsetYDots < -MaxOffsetDots || s.OffsetYDots > MaxOffsetDots {
		fail("printer.options.offset_y",
			"décalage vertical de %d dots : la commande SBPL <A3> porte quatre chiffres, "+
				"soit -%d à %d dots", s.OffsetYDots, MaxOffsetDots, MaxOffsetDots)
	}
	if s.Copies < MinConfiguredCopies || s.Copies > MaxConfiguredCopies {
		fail("printer.options.copies",
			"%d exemplaires : le nombre d'exemplaires va de %d à %d sur un poste libre-service, "+
				"où un client colle une étiquette sur un sac",
			s.Copies, MinConfiguredCopies, MaxConfiguredCopies)
	}
	return faults
}

// Head describes the print head a frame is addressed to.
//
// It is not a configuration block: nothing here is an operator's decision. It is the
// hardware fact the frame has to fit inside, and the two numbers come from the WS4
// manual.
type Head struct {
	// DotsPerMM is the pitch of the head: 8 on a WS408 (203 dpi), 12 on a WS412.
	//
	// It is CHECKED against template.media.dots_per_mm and never replaces it: the
	// resolution of this application has a single source, and it is the template
	// (mineur-3, §7.3).
	DotsPerMM float64
	// MaxWidthBytes is the widest <G> block the head accepts, in BYTES: 104 on the
	// WS408, that is 832 dots (§8.3).
	MaxWidthBytes int
}

// WS408 is the head of the whole parc: 8 dots/mm, 104 bytes of <G> block.
//
// There is deliberately no WS412 constructor beside it. Its pitch is known — 12
// dots/mm, stated in §8.2 — but the width of its head in <G> bytes is NOT in any
// document this repository holds, and a driver that guessed it would refuse or accept
// the wrong templates. It is one line to add the day the figure comes from the manual.
func WS408() Head { return Head{DotsPerMM: 8, MaxWidthBytes: 104} }

// Validate refuses a head that could not describe any real printer.
func (h Head) Validate() []domain.Fault {
	var faults []domain.Fault
	if h.DotsPerMM <= 0 {
		faults = append(faults, domain.Fault{Field: "printer.head.dots_per_mm",
			Message: fmt.Sprintf("%g dot par mm : la résolution de la tête est ce qui donne au bitmap "+
				"sa taille physique", h.DotsPerMM)})
	}
	if h.MaxWidthBytes <= 0 {
		faults = append(faults, domain.Fault{Field: "printer.head.max_width_bytes",
			Message: fmt.Sprintf("%d octet de large : une tête d'impression a une largeur",
				h.MaxWidthBytes)})
	}
	return faults
}
