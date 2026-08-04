package domain

import (
	"fmt"
	"strings"
)

// This file holds the ENTRY POINT of the 48 controls a configuration has to pass,
// and the ones that judge THE HARDWARE OF THE STATION AND THE LABEL IT PRINTS: the
// number it answers to, the address it listens on, the three drivers it names, the
// options they declare, the numbering plan, and the template with its offsets.
//
// What judges the SETTINGS a cooperative chose -- the price grid, the weighing
// bounds, the catalog, the retention -- is validate_settings.go. The two halves are
// one list: Validate calls them in the order §11.3 numbers them, and never in the
// order they are written.
//
// THE ORDER IS PART OF THE CONTRACT. A volunteer reads the faults top to bottom and
// §11.3 names its controls by number, so the sequence Validate produces is what a
// screen, a test and a piece of documentation all agree on. Two numbers -- 37 and 47
// -- are holes left on purpose, and each says at its place why it was removed.
//
// Nothing here reads a clock, opens a file or a socket. The two questions a pure
// function cannot answer -- "does this path exist?", "is this print queue really
// enumerated?" -- arrive through Registries.

// retiredScaleTypes are the two values that LEFT the scale enumeration (§9.3),
// each with the reason it left.
//
// The previous version mixed two protocols, a DEGRADED MODE and a TEST TOOL in one
// drop-down list shown to a volunteer. The same state was then reachable through
// three doors -- a configuration value, an automatic fallback, a troubleshooting
// button -- which made the only question that matters on a bad morning undecidable:
// why is this station in manual entry? Refusing the two values is what keeps the
// three questions separate.
var retiredScaleTypes = map[string]string{
	SourceManual: "« manual » est un ÉTAT, pas un protocole : un poste sans balance se déclare avec scale.present = false, et la saisie à la main s'autorise avec manual_entry_allowed",
	SourceReplay: "« replay » est un outil de diagnostic (openscale capture / openscale replay, bouton « Rejouer cette trame »), il n'a rien à faire dans la liste du matériel de pesée",
}

// serialTransports are the transport names control 42 refuses for a printer.
var serialTransports = []string{"serial", "rs232", "rs-232", "com"}

// faultList accumulates the faults one group of controls raises.
//
// It carries the two shorthands Validate used to open as closures over its own
// slice, and it is what lets each group of controls be a function of its own
// without every one of them re-declaring them.
type faultList []Fault

// add appends a fault, naming the field that carries it.
func (f *faultList) add(field, format string, args ...any) {
	*f = append(*f, Fault{Field: field, Message: fmt.Sprintf(format, args...)})
}

// addChoice appends a fault that also names the values the field would accept.
func (f *faultList) addChoice(field string, values []string, format string, args ...any) {
	*f = append(*f, Fault{
		Field: field, Message: fmt.Sprintf(format, args...), Values: values,
	})
}

// Validate returns ALL the faults, not the first one: the administration screen is
// used by volunteers, it must report everything at once, in French, with the
// offending field named and, whenever possible, the list of available values in
// Fault.Values.
//
// reg carries the driver descriptors, which is what allows the options of each
// driver to be validated instead of just its type; an empty registry validates the
// form and not the existence.
//
// An invalid configuration NEVER kills the process (§11.3): the server starts in
// "invalid configuration" mode, loads NeutralProfile in memory WITHOUT writing,
// serves this list of faults and shows a full-screen « Poste en configuration
// d'usine (ERR-CFG-01) ». A broken configuration must never produce a black screen.
func (c *Config) Validate(reg Registries) []Fault {
	// Controls 21, 29 and 38 all read the label geometry, and twenty controls sit
	// between the first and the last: it is resolved once, here, and handed down.
	label := c.labelGeometry(reg)

	var faults []Fault
	faults = append(faults, c.validateStation()...)                                       // 1
	faults = append(faults, c.validateNetwork()...)                                       // 2
	faults = append(faults, c.validateDeclaredDrivers(reg)...)                            // 3-5
	faults = append(faults, c.validateDriverOptions(reg)...)                              // 6-9
	faults = append(faults, c.validatePricing()...)                                       // 10-16
	faults = append(faults, validateNumberingPlan(internalPlan)...)                       // 17-19
	faults = append(faults, c.validateRetiredKeys()...)                                   // 20
	faults = append(faults, c.validateResolution(label)...)                               // 21
	faults = append(faults, c.validateLimits()...)                                        // 22-25
	faults = append(faults, c.validateStability()...)                                     // 26-28
	faults = append(faults, c.validateTemplate(reg, label)...)                            // 29
	faults = append(faults, c.validateJournal()...)                                       // 30
	faults = append(faults, c.validateAdminSecrets()...)                                  // 31
	faults = append(faults, c.validateCatalogShelving()...)                               // 32-36
	faults = append(faults, c.validateLabelOffsets(label)...)                             // 38
	faults = append(faults, c.validateCatalogGuards(reg)...)                              // 39-40
	faults = append(faults, c.validatePrinterDevice()...)                                 // 41-42
	faults = append(faults, CheckPrice("limits.max_amount_cents", c.Limits.MaxAmount)...) // 43
	faults = append(faults, c.validateCatalogImages(reg)...)                              // 44-45
	faults = append(faults, c.validateDropDirectory(reg)...)                              // 46
	faults = append(faults, c.validateUpdate()...)                                        // 48
	faults = append(faults, c.validateGrid()...)                                          // 49
	faults = append(faults, c.validateChipThreshold()...)                                 // 50
	return faults
}

// labelGeometry is what controls 21, 29 and 38 read about the label this station
// prints: the template printer.template names, whether its resolution can be
// divided by, and the head the printer driver declares.
//
// Resolving it is a pair of registry lookups and nothing else -- no control judges
// anything here, the three that need it still do.
type labelGeometry struct {
	template Template
	// exists is whether the registry carries a layout under that name at all.
	exists bool
	// resolutionUsable is what control 21 answers: a template that exists AND declares
	// a pitch every geometric rule can divide the world by.
	resolutionUsable bool
	// head is the geometry the printer driver declares, with every figure it left
	// unsaid filled in from the WS408 of the parc.
	head PrinterCapabilities
}

func (c *Config) labelGeometry(reg Registries) labelGeometry {
	template, exists := reg.Template(c.Printer.Template)
	return labelGeometry{
		template:         template,
		exists:           exists,
		resolutionUsable: exists && template.Media.DotsPerMM > 0,
		head:             reg.PrinterHead(c.Printer.Type).orReference(),
	}
}

// validateStation is control 1: station.number ∈ [1,99]. It is what the watched
// file name derives from.
func (c *Config) validateStation() []Fault {
	var faults faultList
	if c.Station.Number < 1 || c.Station.Number > 99 {
		faults.add("station.number", "%d hors bornes [1, 99] : c'est de ce numéro que dérive le nom du fichier surveillé, flv_<n>.csv",
			c.Station.Number)
	}
	return faults
}

// validateNetwork is control 2: network.listen parseable.
func (c *Config) validateNetwork() []Fault {
	var faults faultList
	if err := checkHostPort(c.Network.Listen); err != nil {
		faults.add("network.listen", "%q n'est pas une adresse hôte:port valide (%s)", c.Network.Listen, err)
	}
	return faults
}

// validateDeclaredDrivers is controls 3 to 5: the three type names a station
// declares are ones this binary carries.
func (c *Config) validateDeclaredDrivers(reg Registries) []Fault {
	var faults faultList

	// 3. scale.type known -- EXACTLY the protocols of the registry (§9.3). WHICH
	//    OPTIONS IT NEEDS IS NOT DECIDED HERE: control 6 asks the schema the chosen
	//    driver declares.
	//
	//    This control used to demand the literal key `scale.options.port` of every
	//    station whose scale.present was raised, whatever its scale.type. A driver
	//    reached by an ADDRESS -- TCP, USB -- was therefore refused before it was ever
	//    asked, on a key its own schema does not carry, and adding one would have meant
	//    editing this function: exactly the coupling §5.2 removes. Nothing moves for the
	//    parc, whose serial drivers declare `port` Required in serial.OptionSchema, and
	//    the volunteer gains a line -- the field counted DOUBLE, once for this rule and
	//    once for the schema.
	switch {
	case c.Scale.Type == "" && c.Scale.Present:
		faults.addChoice("scale.type", reg.ScaleTypes(), "aucun protocole n'est déclaré alors que le poste déclare une balance")
	case c.Scale.Type == "":
		// A station that declares it has no scale names no protocol, and that is
		// deliberate: the neutral profile must not name a piece of hardware.
	default:
		if reason, retired := retiredScaleTypes[c.Scale.Type]; retired {
			faults.addChoice("scale.type", reg.ScaleTypes(), "%q n'est plus une valeur de scale.type : %s", c.Scale.Type, reason)
		} else if available := reg.ScaleTypes(); len(available) > 0 && !known(available, c.Scale.Type) {
			faults.addChoice("scale.type", available, "protocole inconnu %q", c.Scale.Type)
		}
	}

	// 4. printer.type known -- exactly the three registered descriptors, raster by
	//    default, sbpl and preview (§8.1, §8.2).
	if c.Printer.Type == "" {
		faults.addChoice("printer.type", reg.PrinterTypes(), "aucun driver d'impression n'est déclaré")
	} else if available := reg.PrinterTypes(); len(available) > 0 && !known(available, c.Printer.Type) {
		faults.addChoice("printer.type", available, "driver d'impression inconnu %q", c.Printer.Type)
	}

	// 5. catalog.type known. "manual" is NOT a source: the drag and drop of the
	//    administration screen writes into local_drop (A4, §10.1).
	switch {
	case c.Catalog.Type == "":
		faults.addChoice("catalog.type", reg.CatalogSourceNames(), "aucune source de catalogue n'est déclarée")
	case c.Catalog.Type == CatalogSourceManual:
		faults.addChoice("catalog.type", reg.CatalogSourceNames(),
			"%q n'est pas une source : le glisser-déposer de l'administration écrit dans %s, et la scrutation fait le reste",
			CatalogSourceManual, CatalogSourceLocalDrop)
	default:
		if available := reg.CatalogSourceNames(); len(available) > 0 && !known(available, c.Catalog.Type) {
			faults.addChoice("catalog.type", available, "source de catalogue inconnue %q", c.Catalog.Type)
		}
	}
	return faults
}

// validateDriverOptions is controls 6 to 9: each option map judged by the schema
// THE CHOSEN DRIVER declares, and the transport named among the registered ones.
func (c *Config) validateDriverOptions(reg Registries) []Fault {
	var faults faultList

	// 6. scale.options validated by the schema the scale driver declares.
	faults = append(faults, validateOptions("scale.options", c.Scale.Options,
		descriptorByID(reg.Scales, c.Scale.Type), reg.Scales)...)

	// 7. printer.options validated by the schema the printer driver declares.
	faults = append(faults, validateOptions("printer.options", c.Printer.Options,
		descriptorByID(reg.Printers, c.Printer.Type), reg.Printers)...)

	// 8. printer.options.transport is one of the registered transports.
	transport, hasTransport := c.Printer.Options.Text("transport")
	if hasTransport && transport != "" {
		if available := reg.TransportNames(); len(available) > 0 && !known(available, transport) {
			faults.addChoice("printer.options.transport", available, "transport inconnu %q", transport)
		}
	}

	// 9. catalog.options validated by the schema the source declares.
	faults = append(faults, validateOptions("catalog.options", c.Catalog.Options,
		descriptorByID(reg.CatalogSources, c.Catalog.Type), reg.CatalogSources)...)
	return faults
}

// validateNumberingPlan reports the faults of controls 17 to 19 on a numbering
// plan.
//
// The internal numbering plan SELF-CHECKS at start-up (§6.2, ADR-028): every declared prefix is
// exactly four digits, 4 + ref + payload + 1 = 13, and no prefix is declared twice.
// init() already panics on a broken plan, so these three can only fail in a test that
// hands over a broken table -- which is exactly why they are a function and not inline
// code: an inconsistent plan must stop the process AT START-UP, never at print time.
//
// It reuses the very check init() runs, so the two can never diverge: what stops the
// process is what the administration screen would explain.
func validateNumberingPlan(plan map[string]PrefixPlan) []Fault {
	if err := validatePlan(plan); err != nil {
		return []Fault{{
			Field:   "barcode.plan",
			Message: fmt.Sprintf("le plan de numérotation interne est incohérent : %s", err),
		}}
	}
	return nil
}

// validateRetiredKeys is control 20: a configuration still carrying a retired key
// -- numbering plan or pricing coefficient -- is REFUSED.
func (c *Config) validateRetiredKeys() []Fault {
	var faults faultList
	for _, path := range c.retired {
		faults.add(path, "clé supprimée : %s", RetiredKeyReason(path))
	}
	return faults
}

// validateResolution is control 21: template.media.dots_per_mm is the SINGLE source
// of resolution (mineur-3).
//
// barcode.resolution_dpi is gone, and every geometric rule divides the world by this
// number.
func (c *Config) validateResolution(label labelGeometry) []Fault {
	var faults faultList
	if label.exists && !label.resolutionUsable {
		faults.add("template.media.dots_per_mm",
			"le gabarit %q ne déclare aucune résolution utilisable (8 sur une WS408, 12 sur une WS412)",
			c.Printer.Template)
	}
	return faults
}

// validateTemplate is control 29: the template EXISTS and Template.Validate()
// passes -- the nine hard rules of §7.5, on the geometry RECOMPOSED with the
// operator's offsets.
//
// They bear on the head THE DRIVER DECLARES: held as constants of the core, the
// inked width and height were counted at 8 dots/mm, so a station whose printer is
// not the WS408 of the parc failed this very control at start-up — §11.3 puts it out
// of service — on a template nobody could make it accept.
func (c *Config) validateTemplate(reg Registries, label labelGeometry) []Fault {
	var faults faultList
	if !label.exists {
		faults.addChoice("printer.template", reg.TemplateNames(), "gabarit inconnu %q", c.Printer.Template)
	} else if label.resolutionUsable {
		shifted := label.template
		shifted.OffsetXDots, _ = intOption(c.Printer.Options, "offset_x")
		shifted.OffsetYDots, _ = intOption(c.Printer.Options, "offset_y")
		for _, fault := range shifted.ValidateOn(label.head, len(c.Pricing.Tiers)) {
			fault.Field = "printer.template." + fault.Field
			faults = append(faults, fault)
		}
	}
	return faults
}

// 37. REMOVED, and its number left as a hole (ADR-044). It bounded printer.options.copies.
//
//	The bound is now declared by the driver that owns the key and applied by control 7,
//	which checks printer.options against the schema THAT driver declares.
//
//	Held here, it named a key of a driver the core cannot see, and it was one of THREE
//	bounds on one figure: this rule and the option schema said [1, 10], while
//	raster.Settings.Validate accepted anything up to the six digits of the <Q> field. The
//	same number therefore got two different answers depending on whether it was checked as
//	a configuration or as a setting, and the disagreement could only be found by reading
//	all three. There is now one constant, raster.MaxConfiguredCopies, declared beside the
//	other bounds of the manual, and nothing moves for the parc.
//
//	What is given up is what control 3 gave up on `port`: on an EMPTY registry --
//	`openscale config validate` on a laptop -- the schema check is skipped altogether, so
//	the bound is no longer applied at validation time. It is still applied where it decides
//	something, at the construction of the driver, and a bound that only a printer's own
//	package can state is worth more than one the core repeats (§5.2, E1).

// validateLabelOffsets is control 38: offset_x/y RECOMPOSED with the geometry of
// the template (mineur-2).
//
// The ±1 dot arrows of the admin screen invite that adjustment, so it must be
// bounded by the geometry and not merely by ±99. The message names the admissible
// maximum instead of just saying no. The margin is the one THIS head leaves: a bound
// counted at another pitch would refuse an adjustment the printer would have accepted.
func (c *Config) validateLabelOffsets(label labelGeometry) []Fault {
	var faults faultList
	if !label.exists || !label.resolutionUsable || label.head.DotsPerMM != label.template.Media.DotsPerMM {
		return faults
	}
	maxX, maxY := label.template.MaxOffsetDotsOn(label.head, len(c.Pricing.Tiers))
	if offset, ok := intOption(c.Printer.Options, "offset_x"); ok && (offset < 0 || offset > maxX) {
		faults.add("printer.options.offset_x",
			"%d dots hors bornes [0, %d] pour le gabarit %q : au-delà, le contenu encré sortirait de l'étiquette",
			offset, maxX, c.Printer.Template)
	}
	if offset, ok := intOption(c.Printer.Options, "offset_y"); ok && (offset < 0 || offset > maxY) {
		faults.add("printer.options.offset_y",
			"%d dots hors bornes [0, %d] pour le gabarit %q : au-delà, le contenu encré sortirait de l'étiquette",
			offset, maxY, c.Printer.Template)
	}
	return faults
}

// validatePrinterDevice is controls 41 and 42: a roll count worth alerting on, and
// the transport a label cannot travel over.
func (c *Config) validatePrinterDevice() []Fault {
	var faults faultList

	// 41. roll_capacity ≥ 50. Below that the 90 % alert would fire on the first
	//     labels of a fresh roll and teach a volunteer to ignore it.
	if capacity, ok := c.Printer.Options.Int("roll_capacity"); ok && capacity < 50 {
		faults.add("printer.options.roll_capacity", "%d est sous le plancher de 50 étiquettes", capacity)
	}

	// 42. A SERIAL transport is forbidden for the printer: a label weighs 16 ko, that
	//     is about 17 s at 9 600 bauds (§8.3).
	transport, hasTransport := c.Printer.Options.Text("transport")
	if hasTransport && known(serialTransports, strings.ToLower(transport)) {
		faults.addChoice("printer.options.transport",
			[]string{TransportWinspool, TransportDevfile, TransportTCP, TransportFile},
			"un transport série est interdit pour l'imprimante : une étiquette pèse 16 ko, soit environ 17 s à 9 600 bauds")
	}
	return faults
}

// intOption reads an option that must be a whole number of dots, and reports
// whether it was there and readable.
func intOption(options DriverOptions, key string) (int, bool) {
	value, ok := options.Int(key)
	return int(value), ok
}
