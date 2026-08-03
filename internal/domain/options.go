package domain

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
)

// This file holds the DRIVER-SPECIFIC half of a configuration: the untyped option
// map a file carries, the schema a driver declares to describe it, and the
// registries a running binary answers "which drivers exist?" from.
//
// It is one subject and not two: an option map means nothing without the schema
// that declares its keys, and a schema means nothing without the descriptor that
// owns it. What JUDGES them lives in validate.go and validate_options.go.

// --- Driver options ------------------------------------------------------------

// DriverOptions is the driver-specific half of a hardware or catalog block.
//
// It stays UNTYPED on purpose: the administration screen generates its form from
// the schema the driver DECLARES (§9.3), so adding a scale model must not mean
// adding a Go field here. The values are kept as raw JSON rather than as `any`
// because decoding into `any` turns every number into a float64, and no float
// carries a quantity in this application.
type DriverOptions map[string]json.RawMessage

// Text reports a string option, and whether it is present and really a string.
func (o DriverOptions) Text(key string) (string, bool) {
	raw, ok := o[key]
	if !ok {
		return "", false
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

// Int reports a whole-number option, and whether it is present and really whole.
func (o DriverOptions) Int(key string) (int64, bool) {
	number, ok := jsonNumber(o[key])
	if !ok {
		return 0, false
	}
	value, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

// jsonNumber decodes a raw value as a JSON number, refusing a QUOTED one.
//
// The refusal is deliberate: encoding/json happily reads a quoted numeric literal
// into a json.Number, so `"baud": "9600"` would pass silently. A configuration that
// spells a baud rate as text has a type error, and the driver form is what must say
// so -- the admin screen offers a numeric field, and a file that came from somewhere
// else has to be told.
func jsonNumber(raw json.RawMessage) (json.Number, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] == '"' {
		return "", false
	}
	var number json.Number
	if json.Unmarshal(trimmed, &number) != nil {
		return "", false
	}
	return number, true
}

// Ratio reports a fractional option, and whether it is present and numeric.
//
// The only floats a configuration carries are RATIOS -- min_readable_ratio,
// max_weighable_drop -- and never a mass, a price or a length.
func (o DriverOptions) Ratio(key string) (float64, bool) {
	number, ok := jsonNumber(o[key])
	if !ok {
		return 0, false
	}
	value, err := number.Float64()
	if err != nil {
		return 0, false
	}
	return value, true
}

// Bool reports a boolean option, and whether it is present and really a boolean.
func (o DriverOptions) Bool(key string) (bool, bool) {
	raw, ok := o[key]
	if !ok {
		return false, false
	}
	var value bool
	if json.Unmarshal(raw, &value) != nil {
		return false, false
	}
	return value, true
}

// Group reports a nested option object, such as printer.options.fallback.
func (o DriverOptions) Group(key string) (DriverOptions, bool) {
	raw, ok := o[key]
	if !ok {
		return nil, false
	}
	var value DriverOptions
	if json.Unmarshal(raw, &value) != nil {
		return nil, false
	}
	return value, true
}

// Has reports whether the option is present, whatever its value.
func (o DriverOptions) Has(key string) bool {
	_, ok := o[key]
	return ok
}

// Keys reports the option names in a stable order, so that two runs of the
// validation produce the faults in the same sequence.
func (o DriverOptions) Keys() []string { return sortedKeys(o) }

// WithText returns the same options with one key set to a string value.
//
// It never touches the receiver, for the reason clone exists: a DriverOptions is a MAP,
// so a copy of a Config shares it with the configuration the station is running on, and
// writing through one of them would change the other.
func (o DriverOptions) WithText(key, value string) DriverOptions {
	next := o.clone()
	if next == nil {
		next = make(DriverOptions, 1)
	}
	// json.Marshal of a string cannot fail: it escapes what it must, and replaces what is
	// not valid UTF-8 rather than refusing it.
	raw, _ := json.Marshal(value)
	next[key] = raw
	return next
}

// clone returns a shallow copy, so that Export can strip a secret without reaching
// into the configuration the station is running on.
func (o DriverOptions) clone() DriverOptions {
	if o == nil {
		return nil
	}
	out := make(DriverOptions, len(o))
	for key, value := range o {
		out[key] = value
	}
	return out
}

// --- What a driver declares ------------------------------------------------------

// OptionKind names the shape one driver option accepts.
type OptionKind uint8

const (
	// OptionText is any string.
	OptionText OptionKind = iota
	// OptionInt is a whole number, bounded by Min and Max when Max is non-zero.
	OptionInt
	// OptionBool is a boolean.
	OptionBool
	// OptionRatio is a fraction between Min and Max expressed in per mille, which is
	// how a ratio gets bounded without a float ever entering a declaration.
	OptionRatio
	// OptionEnum is one of Values.
	OptionEnum
	// OptionHostPort is a host:port pair.
	OptionHostPort
	// OptionURL is an absolute http or https URL.
	OptionURL
	// OptionGroup is a nested object whose own schema is Options.
	OptionGroup
)

// String reports the kind the way a fault names it, in French.
func (k OptionKind) String() string {
	switch k {
	case OptionText:
		return "texte"
	case OptionInt:
		return "nombre entier"
	case OptionBool:
		return "vrai ou faux"
	case OptionRatio:
		return "nombre"
	case OptionEnum:
		return "valeur d'une liste"
	case OptionHostPort:
		return "hôte:port"
	case OptionURL:
		return "URL http ou https"
	case OptionGroup:
		return "objet"
	}
	return "inconnu"
}

// OptionUse names what a value DESIGNATES, when knowing that lets a control judge it
// without knowing which driver declared the key.
//
// Kind says what SHAPE a value has — text, a whole number, a web address. Use says what
// it POINTS AT, and only the second lets Config.Validate probe a directory or refuse an
// HTTP host on a key it has never heard of.
//
// It exists because the three controls that did this work were three `if` statements
// naming `local_drop` and `webdav` INSIDE THE DOMAIN: a third catalog source could not
// be added without editing this file, which is the exact opposite of a plug-in point.
// The guards themselves have not moved an inch — what moved is who declares them
// (ADR-052).
type OptionUse uint8

const (
	// UseNone is the zero value, and what almost every option declares: the schema says
	// nothing beyond the shape of the value.
	UseNone OptionUse = iota
	// UseDropDirectory is a directory ON THIS MACHINE the service must be able to list,
	// write into and delete from — the acknowledgement of §10.1 IS a deletion.
	//
	// It carries the guard of important-11: a value that names an HTTP(S) host is refused
	// outright. A "local" directory reached through an account and a password is the Z:
	// drive of the legacy application under another name, and a source that fetches from
	// a share is a different source, with a different acknowledgement.
	UseDropDirectory
)

// OptionSchema declares one option of a driver.
//
// It is what lets the administration screen GENERATE its form and the validation
// check the OPTIONS of a driver instead of only its type name: `port` among the
// enumerated ports, `queue` among the queues REALLY visible, `address` as
// host:port (§11.3).
type OptionSchema struct {
	Key      string
	Kind     OptionKind
	Required bool
	// Use is what the value points at, when a control can act on knowing it. Almost every
	// option leaves it at UseNone.
	Use OptionUse
	// Values is the closed list of an enum, and for an option the platform can
	// enumerate -- a serial port, a print queue -- the values it REALLY found. An
	// empty list means "we could not enumerate": the form is checked, membership is
	// not.
	Values []string
	// Min and Max bound an OptionInt, and an OptionRatio IN PER MILLE. Both zero
	// means unbounded.
	Min, Max int64
	// Options is the schema of a nested OptionGroup.
	Options []OptionSchema
}

// DriverDescriptor is what validating a configuration needs to know about a
// driver: its registry key, the wording a volunteer reads, and the schema of its
// options.
type DriverDescriptor struct {
	// ID is the registry key, the value that goes into the file: "gram-xfoc-plus".
	ID string
	// Label is what the drop-down list shows, in French: "GRAM XFOC +".
	Label   string
	Options []OptionSchema
	// Capabilities is what a PRINTER driver declares about the head it drives, and it
	// is what controls 29 and 38 measure a template against.
	//
	// The zero value is what every other kind of driver leaves here, and also what a
	// printer that inks no paper declares: the rules then bear on ReferenceHead, the
	// WS408 of the parc.
	Capabilities PrinterCapabilities
	// SelfTests are the built-in patterns of §8.6 a PRINTER driver honours, by the name
	// the troubleshooting route sends: "label", "alignment", "ruler".
	//
	// Plain strings, and not a type of their own: the catalogue of the three lives in
	// internal/printing, which is where their wording, their access level and what each
	// print settles are written. What crosses into the domain is WHICH ONES a driver
	// honours, so that the administration screen offers no button whose only possible
	// answer is a refusal (ADR-025).
	//
	// Nil means « this binary cannot say », which is the honest answer of a validation
	// run with no driver registry at all; an EMPTY slice is the assertion « none ».
	SelfTests []string
	// DeviceKey is the printer.options key a TRANSPORT descriptor reads to DESIGNATE ITS
	// DEVICE: DeviceKeyQueue for winspool, DeviceKeyPath for devfile and file,
	// DeviceKeyAddress for tcp. Empty on every other kind of descriptor.
	//
	// It travels for the reason Endpoint does just below, and it was learnt the same way.
	// The Matériel screen carried ONE device field, wired to `queue` whatever the transport
	// was; « Rechercher l'imprimante » proposes hosts answering on port 9100, and clicking
	// one wrote 192.168.0.43:9100 into printer.options.queue. Nothing refused it — `queue`
	// is a key of the driver, and no control ties a key to a transport — so the station
	// saved a configuration that could not print, and said so only when the socket was
	// opened.
	//
	// Declaring it here is what lets the screen ask the STATION where to write instead of
	// carrying a table of its own: a fifth transport is then one line in a registry, and the
	// form follows.
	DeviceKey string
	// Endpoint is the kind of access point a SCALE driver is reached and recognised on:
	// EndpointSerialPort, or empty for a protocol that names none.
	//
	// It travels with the descriptor for the same reason SelfTests does — a screen and a
	// diagnosis must read what the DRIVER declares instead of assuming. `openscale
	// doctor` checked « le port série est présent et ouvrable » on every station that
	// declares a scale, reading scale.options.port whatever the protocol was: on a scale
	// reached any other way that control was a red light on a key that does not exist.
	Endpoint string
}

// The kinds of access point a scale protocol can be reached on, spelled once.
//
// They live in the domain because a descriptor carries them across to `openscale doctor`
// and to the administration screen, and a second spelling on the far side is how a
// declaration and its reader stop meaning the same thing.
const (
	// EndpointSerialPort is one serial port of the machine, as the platform enumerates
	// them.
	EndpointSerialPort = "serial-port"
	// EndpointNone is a protocol that declares no access point of a kind this
	// application enumerates: it is chosen by hand and never detected.
	EndpointNone = "none"
)

// --- Registries ------------------------------------------------------------------

// PathChecker answers the questions a pure validation cannot: what can this path do
// FROM THE CONTEXT OF THE SERVICE?
//
// It is an interface declared on the consumer side, and a nil one is a legitimate
// state: `openscale config validate` on a laptop cannot know what the service
// account sees. The form is then validated and existence is not.
type PathChecker interface {
	// Readable reports nil when the service could read that path.
	Readable(path string) error
	// Droppable reports nil when the service could create AND DELETE a file there.
	//
	// Two questions and not one: a catalog is acknowledged by DELETING it (ADR-004), so a
	// directory the service may only read would make the same import loop for ever --
	// applied, archived, and still there at the next poll.
	Droppable(path string) error
}

// Registries carries the driver descriptors and the templates a running binary
// knows.
//
// It exists so that the validation can check the OPTIONS of each driver and not
// merely say "unknown type". An EMPTY registry is a legitimate state -- the drivers
// are delivered by later lots, and `openscale config validate` may run outside the
// service: membership is then not checked and the message says so. What is always
// checked is the FORM, and the values that were RETIRED with a written reason.
type Registries struct {
	Scales         []DriverDescriptor
	Printers       []DriverDescriptor
	Transports     []DriverDescriptor
	CatalogSources []DriverDescriptor
	// Templates is the label layouts this binary can load. Nil means "the templates
	// compiled into the binary", which is where they live until L4.
	Templates map[string]Template
	// Paths probes the filesystem for controls 44 and 46. Nil means "we cannot know".
	Paths PathChecker
}

// ScaleTypes reports the scale protocols a volunteer may choose from.
func (r Registries) ScaleTypes() []string { return descriptorIDs(r.Scales) }

// PrinterTypes reports the printer drivers a volunteer may choose from.
func (r Registries) PrinterTypes() []string { return descriptorIDs(r.Printers) }

// TransportNames reports the byte transports a volunteer may choose from.
func (r Registries) TransportNames() []string { return descriptorIDs(r.Transports) }

// CatalogSourceNames reports the catalog sources a volunteer may choose from.
func (r Registries) CatalogSourceNames() []string { return descriptorIDs(r.CatalogSources) }

// PrinterHead reports the geometry the driver printer.type names declares about its
// head.
//
// An unknown driver — and an EMPTY registry, which `openscale config validate` on a
// laptop legitimately is — answers a head that declares nothing, and the rules then
// fall back on the label of the parc rather than on nothing at all.
func (r Registries) PrinterHead(id string) PrinterCapabilities {
	if descriptor := descriptorByID(r.Printers, id); descriptor != nil {
		return descriptor.Capabilities
	}
	return PrinterCapabilities{}
}

// TemplateNames reports the label layouts this binary can load, in a stable order.
func (r Registries) TemplateNames() []string { return sortedKeys(r.templates()) }

// Template returns a layout by name, and whether it exists.
func (r Registries) Template(name string) (Template, bool) {
	template, ok := r.templates()[name]
	return template, ok
}

// templates falls back on the layouts compiled into the binary, which is where
// they live until the rendering engine turns them into files (templates.go).
func (r Registries) templates() map[string]Template {
	if r.Templates != nil {
		return r.Templates
	}
	return ShippedTemplates()
}

func descriptorIDs(list []DriverDescriptor) []string {
	if len(list) == 0 {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, descriptor := range list {
		out = append(out, descriptor.ID)
	}
	sort.Strings(out)
	return out
}

func descriptorByID(list []DriverDescriptor, id string) *DriverDescriptor {
	for i := range list {
		if list[i].ID == id {
			return &list[i]
		}
	}
	return nil
}

// optionsUsedAs reports the options a named driver declares for a given use.
//
// It is what lets a control act on WHAT A VALUE POINTS AT without naming a driver: the
// key that carries a drop directory is `directory` in the source shipped today and may be
// anything in the next one, and the validation is not entitled to a second copy of that
// decision. An unknown driver yields nothing, which is the honest behaviour of a
// validation run against a registry that does not carry it.
func optionsUsedAs(list []DriverDescriptor, id string, use OptionUse) []OptionSchema {
	descriptor := descriptorByID(list, id)
	if descriptor == nil {
		return nil
	}
	var out []OptionSchema
	for _, schema := range descriptor.Options {
		if schema.Use == use {
			out = append(out, schema)
		}
	}
	return out
}

// sourcesFetchingByURL reports the sources that go and GET the catalog from an address.
//
// It is the suggestion control 39 offers when somebody types a web address into a drop
// path: « choose the source that fetches from a share » is only useful if it can say
// which one that is, and reading the schemas answers it for a source that did not exist
// when the control was written.
func sourcesFetchingByURL(list []DriverDescriptor) []string {
	var out []string
	for _, descriptor := range list {
		for _, schema := range descriptor.Options {
			if schema.Kind == OptionURL {
				out = append(out, descriptor.ID)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// driversDeclaring reports which OTHER drivers of a list declare a given option key.
//
// It turns « option inconnue du driver "webdav" » into « … c'est "local_drop" qui la
// déclare », which is the difference between a refusal and a piece of advice — and it
// does it for every driver family and every key, where the control it replaces knew one
// key and two sources by name.
func driversDeclaring(list []DriverDescriptor, key, except string) []string {
	var out []string
	for _, descriptor := range list {
		if descriptor.ID == except {
			continue
		}
		for _, schema := range descriptor.Options {
			if schema.Key == key {
				out = append(out, descriptor.ID)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}
