// Package example is a printer driver written to be COPIED, and registered nowhere.
//
// It is a complete ports.Printer: it declares an identity, a head geometry, an option
// schema, one self-test and a factory, it refuses what §8.5 says to refuse with the kind
// §8.5 says to refuse it with, and it passes internal/printing/conformance unchanged. What
// it does with the frame it builds is the only thing that is not real — the bytes go into a
// buffer in memory instead of a device — because a test owns no printer.
//
// # How to use it
//
// Copy the directory, rename the package, then follow the TODO(driver) markers: there is
// one on every point of variation and nowhere else. docs/07-ajouter-un-materiel.md walks
// the same path in French, with the commands.
//
// # Why it is not registered, and must not be
//
// cmd/openscale/drivers.go registers the drivers a station can name in printer.type. An
// entry there is a line in a drop-down list a volunteer picks from, so registering this one
// would offer them a printer that prints nothing — the reasoning drivers.go already applies
// to `sbpl`, which §8.1 names and no station carries. cmd/openscale/drivers_test.go asserts
// the absence.
//
// # Two shapes, and which one this is
//
// A printer driver picks one of two shapes, and the choice settles three declarations at
// once — where the bytes go, what geometry is declared, and what Subject.DrivesAHead says:
//
//   - IT REACHES A DEVICE. It declares the `transport` key in its own option schema and
//     receives a ports.Transport in printing.DriverConfig, built and closed by the
//     composition root — a driver that opened a device itself would lose « one frame, four
//     destinations » (§8.4). It declares the MEASURED geometry of its head, it refuses a
//     template drawn for another pitch, and its Subject sets DrivesAHead.
//     internal/printing/raster is that shape.
//   - IT PRODUCES SOMETHING TO LOOK AT. It declares NO transport — the composition root then
//     builds none — and takes its directory from DriverConfig.OutputDir rather than
//     inventing a path. It inks no paper, so it declares NO GEOMETRY, refuses no template —
//     no template is foreign to it — and its Subject leaves DrivesAHead false.
//     internal/printing/preview is that shape, and ships.
//
// THIS DRIVER IS THE SECOND SHAPE, minus the directory: it writes into a buffer, because a
// test owns no printer. Copy it for a driver that produces files; copy raster for one that
// drives a head, and take the three figures off YOUR paper.
package example

import (
	"fmt"

	"openscale/internal/domain"
	"openscale/internal/printing"
	"openscale/internal/station/ports"
)

// ID is the registry key and would be the value of printer.type.
//
// Lower case, digits and hyphens: the registry lookup is an exact string comparison, so a
// capital or a trailing blank makes a driver a configuration file cannot name.
//
// TODO(driver): name your driver after the HARDWARE, as `raster` and `sbpl` are named after
// what they emit — never after the shop, the station or the person who wrote it.
const ID = "example-printer"

// Label is what a volunteer reads in the drop-down list of the administration screen.
//
// It says what the driver DOES and not only what it is called, because two entries of that
// list are one wrong click apart and a station set on the wrong one goes on saying
// « Étiquette envoyée à l'imprimante » while nothing comes out.
//
// TODO(driver): the name printed on the device, or a French sentence saying what it does.
const Label = "Exemple — écrit en mémoire, n'imprime rien"

// THIS DRIVER DECLARES NO HEAD GEOMETRY, and the three zeros are an ASSERTION rather than
// an omission: it inks no paper, so it has neither a resolution nor a printable area of its
// own. The hard rules of §7.5 then bear on domain.ReferenceHead, and they are NOT suspended.
//
// It carried the 8 dots/mm and the 280 × 200 dots of the WS408 for exactly as long as it
// took somebody to point out what copying them costs. Those three figures travel through
// printing.Registry.Descriptors into domain.Registries.PrinterHead, where controls 29 and 38
// of §11.3 measure a template against them — so a package that had never touched a WS408 was
// about to let every station naming it validate a label against a head nobody owns. The
// conformance suite passed 18 out of 18 the whole time: it prints the template the subject
// declares, so the copy satisfied the check by being a copy.
// cmd/openscale/drivers_test.go now refuses that exact coincidence.
//
// TODO(driver): a driver that DRIVES A HEAD declares three MEASURED figures — the pitch off
// the `ruler` self-test, the inked area off `alignment`, both read on paper — and refuses a
// template drawn for another pitch. internal/printing/raster carries both, and
// checkTemplateHead is the function to copy. A measurement does not land on a WS408 to the
// dot by accident; if yours does, you copied it.

// MaxCopies is the ceiling this driver honestly accepts, and it is a fact about the WIRE.
//
// Here it is the two digits the frame header carries. On the raster driver it is the six
// digits of the SBPL <Q> field. It is never a shop policy: a job past it is refused with the
// value it was given, never rounded into something that prints.
//
// TODO(driver): the widest count your own frame can express.
const MaxCopies = 99

// MinCopies is the smallest count that means anything: a job printing zero labels is a
// customer holding a bag and nothing else.
const MinCopies = 1

// The keys of printer.options, spelled exactly as config.json carries them (§11.2).
//
// They are declared once, HERE, because OptionSchema and ParseOptions must never drift
// apart: the form the administration screen generates and the parser that reads the file
// back are two halves of one contract. Spelled in two packages, a key renamed on one side
// becomes a field the form offers and the driver never reads.
const (
	optionCopies = "copies"
	optionHeader = "header"
)

// Driver is the registry entry of this driver, and the ONE value a composition root would
// take from this package (§5.2).
//
// It is COMPLETE — identity, capabilities, option schema, self-tests and factory — because
// what a driver needs from a configuration is the driver's own business. A composition root
// that spelled the option keys of a printer would have to be edited for every option that
// printer ever gains, which is exactly the coupling §5.2 removes.
func Driver() printing.Driver {
	return printing.Driver{
		Descriptor: domain.PrinterDescriptor{
			ID:    ID,
			Label: Label,
			Capabilities: domain.PrinterCapabilities{
				// The whole label is a bitmap, symbol included. That is what a raster
				// driver is, and it is what this example draws.
				Raster: true,
				// FALSE, and the honesty of that flag is checked: a driver that declares
				// Status must be able to ASK the device. This one writes into memory and
				// has no return channel, so Status answers PrinterUnknown — see
				// (*Printer).Status.
				//
				// TODO(driver): true only if your transport is bidirectional and your
				// printer really answers a status query (§8.5, level N3).
				Status: false,
				// TODO(driver): true only if the device cuts. None of the eleven commands
				// of §8.3 does, and no printer of the parc is a cutter (§19).
				Cutter:    false,
				MaxCopies: MaxCopies,
				// DotsPerMM, InkedWidthDots and InkedHeightDots are left at ZERO on
				// purpose. See the block above MaxCopies: they are the three figures a
				// copied driver must never inherit.
			},
		},
		Options: OptionSchema(),
		// ONE name out of the three of §8.6, and a short list is an ASSERTION rather than
		// an omission: the Matériel page draws its buttons from this list, so a pattern
		// declared and not produced is a button that fails on the click, and one produced
		// and not declared is a self-test no volunteer can ever launch (ADR-025).
		//
		// TODO(driver): add printing.SelfTestAlignment and printing.SelfTestRuler once you
		// really draw them — internal/printing/raster/selftest.go is the file to copy. The
		// alignment pattern settles the polarity of the bitmap and the registration of the
		// media, the ruler settles the pitch the head really prints at; both are read off
		// PAPER, so neither can be written without a printer on the bench.
		SelfTests: []printing.SelfTest{printing.SelfTestLabel},
		New: func(c printing.DriverConfig) (ports.Printer, error) {
			settings, err := ParseOptions(c.Options)
			if err != nil {
				return nil, err
			}
			// c.Transport is nil here and that is the agreement, not an oversight: the
			// composition root builds the byte layer ONLY for a driver whose own schema
			// names the `transport` key, and this one does not.
			//
			// c.OutputDir, on the contrary, is ALWAYS filled — a directory opens nothing
			// and costs nothing to hand over, so the root passes it without consulting any
			// schema. This driver ignores it because it writes into memory; a driver that
			// produces files takes its directory from there and NEVER builds a path of its
			// own.
			return New(Options{
				Clock:     c.Clock,
				Log:       c.Log,
				Template:  c.Template,
				Settings:  settings,
				DemoLabel: c.DemoLabel,
			})
		},
	}
}

// OptionSchema declares printer.options, which is what lets the administration screen
// GENERATE its form and control 7 of Config.Validate check the OPTIONS of this driver
// instead of only its type name (§11.3).
//
// It belongs to this package because the bounds it declares are facts about THIS driver.
// A schema written anywhere else goes stale in silence the day the hardware changes — with
// a form offering a volunteer a range no printer honours, and a value the driver then
// refuses at construction.
//
// TODO(driver): declare every key you read, and nothing else. A key you read and do not
// declare is refused by control 7 as an unknown option, on a station that was configured
// correctly; a key you declare and do not read is a field a volunteer fills in for nothing.
func OptionSchema() []domain.OptionSchema {
	return []domain.OptionSchema{
		// REQUIRED and BOUNDED. Required, because the zero value is not a configuration:
		// a copy count of zero is not « one », it is a station that prints nothing. Bounded,
		// because the form the screen generates is built from Min and Max, and a value out
		// of range must be a fault in the all-at-once list of §11.3 — next to the field
		// that carries it — rather than a refusal with a customer standing at the scale.
		{Key: optionCopies, Kind: domain.OptionInt, Required: true,
			Min: MinCopies, Max: MaxCopies},
		// OPTIONAL, and its absence has a meaning of its own: no header. That is the one
		// case where a missing key and a zero value may be read as the same thing, and it
		// is legitimate only because false is a usable answer.
		{Key: optionHeader, Kind: domain.OptionBool},
	}
}

// Settings is printer.options once read: what this driver keeps from a configuration.
type Settings struct {
	// Copies is how many copies a job that names none asks for. See (*Printer).copiesFor.
	Copies int
	// Header prefixes every frame with one readable line naming the job. It is what makes
	// a buffer legible in a test; a real frame would carry it in whatever the printer
	// language calls a job header, or not at all.
	Header bool
}

// DefaultSettings is what this driver runs on when nobody has chosen.
//
// TODO(driver): there is deliberately no such thing for darkness, speed and the copy count
// of a REAL printer — §8.2 sets those on a real print run, which is why the raster driver
// declares them required and has no default at all. A default is legitimate only for a
// setting nobody has to measure.
func DefaultSettings() Settings { return Settings{Copies: 1, Header: true} }

// ParseOptions turns the driver options of config.json into the settings above.
//
// A value that is PRESENT but of the wrong type is an ERROR and never a silent default:
// `"copies": "1"` is a type error a volunteer has to be told about, not a copy count
// (§11.2). A key that is ABSENT is a different thing again, and the two are told apart on
// purpose — the shared serial link once left four fields at the Go zero value because the
// completion that fills them was never called on that path, and the detection could not
// succeed on any port of any machine.
//
// The messages are FRENCH: they reach the administration screen and the technical journal,
// where somebody who is not a developer reads them.
func ParseOptions(o domain.DriverOptions) (Settings, error) {
	var settings Settings

	if !o.Has(optionCopies) {
		return Settings{}, fmt.Errorf(
			"printer.options.%s : le nombre d'exemplaires est obligatoire, de %d à %d",
			optionCopies, MinCopies, MaxCopies)
	}
	copies, ok := o.Int(optionCopies)
	if !ok {
		return Settings{}, fmt.Errorf(
			"printer.options.%s : un nombre entier est attendu, sans guillemets", optionCopies)
	}
	if copies < MinCopies || copies > MaxCopies {
		return Settings{}, fmt.Errorf(
			"printer.options.%s : %d exemplaire(s) demandé(s), la plage va de %d à %d",
			optionCopies, copies, MinCopies, MaxCopies)
	}
	settings.Copies = int(copies)

	if o.Has(optionHeader) {
		header, ok := o.Bool(optionHeader)
		if !ok {
			return Settings{}, fmt.Errorf(
				"printer.options.%s : true ou false, sans guillemets", optionHeader)
		}
		settings.Header = header
	}
	return settings, nil
}
