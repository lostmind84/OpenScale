package printing

import (
	"errors"
	"fmt"
	"strings"
)

// SelfTest names one of the three built-in patterns of §8.6.
//
// A named type rather than a bare string: these three values travel from an HTTP query
// parameter (`?what=alignment|ruler`) down to a driver, and a typed constant is what
// stops a fourth spelling from appearing halfway along.
type SelfTest string

// The three self-tests of §8.6, spelled as the troubleshooting route sends them.
const (
	// SelfTestLabel prints a complete demonstration label — ail, 1,236 kg, double tarif.
	// The gesture that goes with it is to lay the result over a current label ON A LIGHT
	// TABLE: that is the acceptance test of A1, and it needs no instrument.
	SelfTestLabel SelfTest = "label"
	// SelfTestAlignment prints a filled 64 × 64 dot square and a one-dot cross in each
	// corner of the printable area. It is the print that SETTLES the polarity of <G> —
	// see ResolvePolarity — and unknown n° 4 of §21 with it.
	SelfTestAlignment SelfTest = "alignment"
	// SelfTestRuler prints a millimetre scale on two edges plus the frame of the
	// printable area. It turns « l'étiquette a l'air un peu courte » into a number.
	SelfTestRuler SelfTest = "ruler"
)

// Access says WHO may launch a self-test, which is a decision and not a detail.
type Access uint8

const (
	// AccessVolunteer is the Dépannage page, WITHOUT a password (§14.4, §14.5,
	// ADR-018). Whoever stands behind the counter can already unplug the printer, so a
	// password there adds no security and removes all the troubleshooting.
	AccessVolunteer Access = iota
	// AccessExpert is the Matériel page of the expert mode, behind « Réglages avancés ».
	// These two are commissioning patterns, not a volunteer's first gesture.
	AccessExpert
)

// String reports the access the way a route table and a log line spell it.
func (a Access) String() string {
	if a == AccessExpert {
		return "expert"
	}
	return "volunteer"
}

// SelfTestInfo is one line of the table of §8.6: what comes out, what it settles, and
// which screen offers the button.
//
// It exists so that the administration screen and the HTTP contract read this list
// instead of holding three hard-coded buttons of their own — the same reason the driver
// registry exists (§5.2).
type SelfTestInfo struct {
	ID SelfTest
	// Button is the wording of the button, in French, exactly as §14.4 lists it.
	Button string
	// Prints is what physically comes out, in French.
	Prints string
	// Lifts is what the print SETTLES, in French. It is the sentence that tells a
	// volunteer why they are burning a label.
	Lifts string
	// Access is the screen the button belongs to.
	Access Access
	// NeedsLabel reports that this self-test cannot exist without a domain.Label built
	// from the catalog and the pricing grid. A printing driver that made up a price
	// would be inventing a number nobody could check, so the label is injected.
	NeedsLabel bool
}

// selfTests is the table of §8.6, in the order §8.6 lists it.
var selfTests = []SelfTestInfo{
	{
		ID:         SelfTestLabel,
		Button:     "Imprimer une étiquette de test",
		Prints:     "une étiquette de démonstration complète (ail, 1,236 kg, double tarif)",
		Lifts:      "le réglage général, le décalage X/Y, et la superposition avec une étiquette actuelle sur une table lumineuse",
		Access:     AccessVolunteer,
		NeedsLabel: true,
	},
	{
		ID:     SelfTestAlignment,
		Button: "Mire d'alignement",
		Prints: "un carré plein de 64 × 64 dots et une croix de 1 dot dans chaque coin de la zone imprimable",
		Lifts:  "la polarité de <G> (invert_bits), le calage du média, et la zone réellement imprimable",
		Access: AccessExpert,
	},
	{
		ID:     SelfTestRuler,
		Button: "Réglette millimétrée",
		Prints: "une réglette millimétrée sur deux bords et le cadre de la zone imprimable",
		Lifts:  "le pas réel de la tête (dots/mm) et le média déclaré",
		Access: AccessExpert,
	},
}

// removedSelfTests are the two self-tests decision A2 DELETED, each with the reason a
// refusal will quote.
//
// They are listed rather than forgotten so that a request naming one gets an answer
// instead of « auto-test inconnu ». Somebody will type them: they are in the old
// documentation, and one of them was a button on the previous screen.
var removedSelfTests = map[SelfTest]string{
	"barcode-frame": "il vérifiait le cadrage de la commande native <BD>, qui n'est plus jamais émise : " +
		"le symbole EAN-13 est tracé DANS le bitmap",
	"character-table": "il relevait la table de caractères du firmware, qui ne sert plus à rien : " +
		"plus aucun texte n'est envoyé en mode natif",
}

// SelfTests returns the three self-tests of §8.6, in the order §8.6 lists them.
//
// The result is a COPY: it is handed to a screen and to a route table, and a catalogue a
// caller can reach into is a catalogue that has stopped describing this binary.
func SelfTests() []SelfTestInfo {
	return append([]SelfTestInfo(nil), selfTests...)
}

// LookupSelfTest finds a self-test by the value a route carries.
//
// The error is FRENCH and it NAMES what exists, never a bare « inconnu » — and for the
// two A2 removed, it says why they are gone rather than pretending never to have heard
// of them.
func LookupSelfTest(what string) (SelfTestInfo, error) {
	for _, t := range selfTests {
		if string(t.ID) == what {
			return t, nil
		}
	}
	if why, removed := removedSelfTests[SelfTest(what)]; removed {
		return SelfTestInfo{}, fmt.Errorf("l'auto-test %q a été supprimé : %s (décision A2, §8.1). "+
			"Les auto-tests disponibles sont %s", what, why, selfTestNames())
	}
	return SelfTestInfo{}, fmt.Errorf("auto-test inconnu %q : les auto-tests disponibles sont %s",
		what, selfTestNames())
}

// selfTestNames is the French tail of every refusal above.
func selfTestNames() string {
	names := make([]string, 0, len(selfTests))
	for _, t := range selfTests {
		names = append(names, string(t.ID))
	}
	return strings.Join(names, ", ")
}

// PolarityReading is what a volunteer SEES on the label the `alignment` self-test just
// produced.
//
// It is a READING and not an opinion: each value describes an appearance, so answering
// takes no understanding of SBPL. That is what makes the trial a measurement — the
// volunteer reports what came out of the printer, and the arithmetic is done here.
type PolarityReading uint8

const (
	// ReadingNone is « nobody has answered yet ». It settles nothing, and it is the zero
	// value so that an unanswered form cannot silently pass for an answer.
	ReadingNone PolarityReading = iota
	// ReadingBlackOnWhite is a BLACK square on a white label: the head burnt the dots
	// the bitmap set, so the polarity used for that print is the right one.
	ReadingBlackOnWhite
	// ReadingWhiteOnBlack is a WHITE square inside a black rectangle — the photographic
	// negative. The head burns the dots the bitmap CLEARS, so the polarity used for that
	// print is the wrong one and invert_bits has to change.
	ReadingWhiteOnBlack
	// ReadingNothing is a blank label, or no label at all. That is not a polarity
	// answer: it is unknown n° 4 falling the other way — the firmware did not take <G>
	// through the queue at all — and it sends the station to the documented GDI fallback
	// rather than to another flag in the configuration.
	ReadingNothing
)

// PolarityQuestion is the closed question the troubleshooting screen asks once the
// `alignment` self-test has printed. French, like everything a volunteer reads.
//
// A closed question with three named appearances, and not « est-ce correct ? » : the
// point of the trial is to SETTLE a hardware ambiguity with one print, and a yes/no
// question would settle it with a guess.
const PolarityQuestion = "Regardez l'étiquette qui vient de sortir. Que voyez-vous ?"

// PolarityAnswer is one admissible answer to PolarityQuestion.
type PolarityAnswer struct {
	Reading PolarityReading
	// Text is what the button says, in French.
	Text string
}

// PolarityAnswers returns the three answers the screen offers, in the order it shows
// them.
func PolarityAnswers() []PolarityAnswer {
	return []PolarityAnswer{
		{ReadingBlackOnWhite, "un carré NOIR sur une étiquette blanche"},
		{ReadingWhiteOnBlack, "un carré BLANC dans un rectangle noir"},
		{ReadingNothing, "rien : l'étiquette est vierge, ou rien n'est sorti"},
	}
}

// ErrNoPolarityReading reports that nobody looked at the label yet.
var ErrNoPolarityReading = errors.New("aucune réponse : la polarité de <G> se lit sur la mire, " +
	"elle ne se devine pas")

// ErrGraphicNotPrinted reports that the graphic block never reached the paper, which is
// a different failure from a wrong polarity and has a different remedy.
var ErrGraphicNotPrinted = errors.New("la mire n'est pas sortie : le bloc graphique <G> n'a pas " +
	"été imprimé du tout. Ce n'est pas un problème de polarité. Vérifiez que la file est bien en " +
	"RAW et que l'imprimante est calibrée pour son rouleau ; si la mire ne sort toujours pas, " +
	"c'est le repli GDI qui s'applique (§19, §21 n° 4)")

// ResolvePolarity turns ONE printed alignment pattern and ONE reading of it into the
// value of printer.options.invert_bits.
//
// This is what « l'auto-test alignment lève la polarité de <G> en 10 min » (§18, L5)
// actually means, and it is worth spelling out because it is easy to read as something
// weaker. The polarity of <G> is a property of the FIRMWARE that no document this
// project holds states: whether a set bit is a burnt dot or a bare one. It cannot be
// derived, and choosing wrong prints every label as its own negative. What settles it
// is a print and a pair of eyes — the square is 64 × 64 dots precisely so that the
// answer is readable across the room — and this function is the arithmetic between the
// two, so that nobody has to reason about negatives while holding a warm label.
//
// printedWith is the polarity the pattern was PRINTED with, which is
// Settings.InvertBits at the time of the print. The answer is therefore relative to
// that print and stays right whichever polarity the station happened to be carrying.
func ResolvePolarity(printedWith bool, reading PolarityReading) (invertBits bool, err error) {
	switch reading {
	case ReadingBlackOnWhite:
		// The square came out as the bitmap draws it: keep what was used.
		return printedWith, nil
	case ReadingWhiteOnBlack:
		// The negative: the head reads the bits the other way round.
		return !printedWith, nil
	case ReadingNothing:
		return false, ErrGraphicNotPrinted
	}
	return false, ErrNoPolarityReading
}
