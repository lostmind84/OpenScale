package raster

import (
	"fmt"

	"openscale/internal/domain"
	"openscale/internal/printing"
	"openscale/internal/printing/transport"
	"openscale/internal/station/ports"
)

// This file is EVERYTHING the composition root has to know about this driver: one call
// to Driver, one line in the registry (§5.2). It carries no wiring of its own — the
// transport is still built by cmd/openscale and handed over, because a driver that
// opened a device would lose the property §8.4 is built on: one frame, four
// destinations.

// The keys of printer.options, spelled exactly as config.json carries them (§11.2).
//
// They are declared once, HERE, because OptionSchema and ParseOptions must never drift
// apart: the form the administration screen generates and the parser that reads the file
// back are two halves of one contract. Spelled in two packages, a key renamed on one
// side becomes a field the form offers and the driver never reads.
const (
	optionTransport    = "transport"
	optionQueue        = "queue"
	optionPath         = "path"
	optionAddress      = "address"
	optionFallback     = "fallback"
	optionEnabled      = "enabled"
	optionDarkness     = "darkness"
	optionSpeed        = "speed"
	optionOffsetX      = "offset_x"
	optionOffsetY      = "offset_y"
	optionInvertBits   = "invert_bits"
	optionCopies       = "copies"
	optionRollCapacity = "roll_capacity"
)

// The area this head puts ink on, in dots, measured on the bench of 28/07/2026: the
// driver of the parc holds 35 × 25 mm of printable area and the stock is 25 mm tall.
//
// The measurement has not moved; who states it has. Held as a constant of the core, it
// was counted at 8 dots/mm and refused AT START-UP every station whose printer is not
// the WS408 of the parc — and §11.3 puts such a station out of service, on a template
// nobody could make it accept (§7.5).
const (
	inkedWidthDots  = 280
	inkedHeightDots = 200
)

// Driver is the registry entry of the production printer driver, and the ONE value
// cmd/openscale registers for this package (§8.1, ADR-002).
//
// It is COMPLETE — identity, capabilities, option schema and factory — for the reason
// gramxfoc.Drivers gives on the weighing side: what a driver needs from a configuration
// is the driver's own business. A composition root that spelled the thirteen keys of
// printer.options would have to be edited for every option this package ever gains,
// which is precisely the coupling §5.2 removes.
//
// The head is a WS408 — 8 dots/mm, 104 bytes of <G> block — because that is the whole
// parc, and WS408 is where the two figures come from rather than from here.
func Driver() printing.Driver {
	head := WS408()
	return printing.Driver{
		Descriptor: domain.PrinterDescriptor{
			ID:    ID,
			Label: Label,
			Capabilities: domain.PrinterCapabilities{
				// The whole label is a bitmap, symbol included. That is the driver.
				Raster: true,
				// It always ANSWERS Status, and the value depends on the transport:
				// PrinterUnknown when it cannot ask (§8.5). See (*Printer).Descriptor.
				Status: true,
				// None of the eleven commands of §8.3 cuts.
				Cutter: false,
				// The bound of the <Q> field, and not a shop policy: see MaxCopies.
				MaxCopies:       MaxCopies,
				DotsPerMM:       head.DotsPerMM,
				InkedWidthDots:  inkedWidthDots,
				InkedHeightDots: inkedHeightDots,
			},
		},
		Options: OptionSchema(),
		// The three of §8.6, and it is the whole of the catalogue because this driver
		// drives a HEAD: the demonstration label, the alignment pattern that settles the
		// polarity of <G>, and the millimetre ruler that settles the pitch. The last two
		// are read off PAPER, which is exactly what this driver produces.
		SelfTests: []printing.SelfTest{
			printing.SelfTestLabel, printing.SelfTestAlignment, printing.SelfTestRuler,
		},
		New: func(c printing.DriverConfig) (ports.Printer, error) {
			settings, err := ParseOptions(c.Options)
			if err != nil {
				return nil, err
			}
			return New(Options{
				Transport: c.Transport,
				Clock:     c.Clock,
				Log:       c.Log,
				Template:  c.Template,
				Settings:  settings,
				Head:      head,
				DemoLabel: c.DemoLabel,
			})
		},
	}
}

// OptionSchema declares printer.options, which is what lets the administration screen
// GENERATE its form and control 7 of Config.Validate check the OPTIONS of the driver
// instead of only its type name (§11.3).
//
// It belongs to this package because the bounds it declares are facts about THIS HEAD:
// MinDarkness, MaxSpeed and MaxOffsetDots are constants of the manual, sitting a few
// lines away in settings.go. A schema written anywhere else goes stale in silence the
// day the head changes — with a form offering a volunteer a range no printer of the parc
// honours, and a value the driver then refuses at construction.
//
// darkness, speed and copies are REQUIRED, and that is the rule Settings states in its
// own words: the zero value is not a configuration, a darkness of zero is not a shade of
// grey. Declaring them required is what makes a file that forgot one produce a fault in
// the ALL-AT-ONCE list of §11.3, next to the field that carries it, instead of a refusal
// with a customer standing at the scale.
//
// queue, path and address are NOT required: each belongs to one transport and is empty
// for the other three. Which one is needed is the transport's business, and each says so
// in French when it is built.
func OptionSchema() []domain.OptionSchema {
	fallback := []domain.OptionSchema{
		{Key: optionEnabled, Kind: domain.OptionBool},
		{Key: optionTransport, Kind: domain.OptionEnum, Values: transport.Names()},
		{Key: optionQueue, Kind: domain.OptionText},
		{Key: optionPath, Kind: domain.OptionText},
		{Key: optionAddress, Kind: domain.OptionHostPort},
	}
	return []domain.OptionSchema{
		{Key: optionTransport, Kind: domain.OptionEnum, Required: true, Values: transport.Names()},
		{Key: optionQueue, Kind: domain.OptionText},
		{Key: optionPath, Kind: domain.OptionText},
		{Key: optionAddress, Kind: domain.OptionHostPort},
		{Key: optionFallback, Kind: domain.OptionGroup, Options: fallback},
		{Key: optionDarkness, Kind: domain.OptionInt, Required: true,
			Min: MinDarkness, Max: MaxDarkness},
		{Key: optionSpeed, Kind: domain.OptionInt, Required: true,
			Min: MinSpeed, Max: MaxSpeed},
		// The offsets are bounded HERE by the four digits of the <A3> field and by
		// control 38 against the geometry of the template, which is the bound that
		// really matters — beyond it the inked content leaves the label.
		{Key: optionOffsetX, Kind: domain.OptionInt, Min: -MaxOffsetDots, Max: MaxOffsetDots},
		{Key: optionOffsetY, Kind: domain.OptionInt, Min: -MaxOffsetDots, Max: MaxOffsetDots},
		{Key: optionInvertBits, Kind: domain.OptionBool},
		// The bound is the one Settings.Validate applies to the same key, and it is the
		// same constant: a schema that refused eleven copies while the driver accepted
		// five hundred would be two answers to one question (see MaxConfiguredCopies).
		{Key: optionCopies, Kind: domain.OptionInt, Required: true,
			Min: MinConfiguredCopies, Max: MaxConfiguredCopies},
		{Key: optionRollCapacity, Kind: domain.OptionInt, Min: 50, Max: 100_000},
	}
}

// ParseOptions turns the driver options of config.json into the settings of §8.2.
//
// IT DELIBERATELY LEAVES THE OFFSET AT ZERO, and that is the whole point of the guard
// checkTheOffsetIsAppliedOnce documents a file away. printer.options.offset_x feeds the
// TEMPLATE, because the template is the only one of the two the preview screen shows: a
// volunteer pressing the ±1 dot arrow must see the label move on the screen they are
// adjusting against. Wired naively, one key would feed both the layout and the <A3>
// command, the label would move twice, and nobody would find out until a roll had been
// spoiled.
//
// A missing darkness, speed or copy count is an ERROR and never a silent default: these
// three are set ON A REAL PRINT RUN, and a file that forgot one must say so rather than
// let the station run on a figure nobody chose. The message is FRENCH — it reaches the
// administration screen and the technical journal.
func ParseOptions(o domain.DriverOptions) (Settings, error) {
	settings := Settings{}
	for _, field := range []struct {
		key  string
		into *int
	}{
		{optionDarkness, &settings.Darkness},
		{optionSpeed, &settings.Speed},
		{optionCopies, &settings.Copies},
	} {
		value, ok := o.Int(field.key)
		if !ok {
			return Settings{}, fmt.Errorf(
				"printer.options.%s : cette valeur se règle sur un tirage réel et le fichier doit la porter",
				field.key)
		}
		*field.into = int(value)
	}
	settings.InvertBits, _ = o.Bool(optionInvertBits)
	return settings, nil
}
