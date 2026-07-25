package serial

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	serialport "go.bug.st/serial"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// The keys of scale.options, spelled exactly as config.json carries them (§11.2).
// They are declared once here because OptionSchema and ParseOptions must never
// drift apart: the form the administration screen generates and the parser that
// reads the file back are two halves of the same contract.
const (
	optionPort       = "port"
	optionBaud       = "baud"
	optionBits       = "bits"
	optionParity     = "parity"
	optionStop       = "stop"
	optionBackoffMin = "backoff_min_ms"
	optionBackoffMax = "backoff_max_ms"
)

// The link defaults of the parc, applied to any field a configuration leaves out.
const (
	defaultBaud   = 9600
	defaultBits   = 8
	defaultParity = "N"
	defaultStop   = 1
	// defaultReadBufferSize is 4 KiB, and NOT the 16 bytes of SetupComm(h, 16, 16):
	// a queue smaller than one 18-byte frame is one of the two reasons the legacy
	// corpus is full of half frames (§9.1).
	defaultReadBufferSize = 4096
	defaultBackoffMin     = 200 * time.Millisecond
	defaultBackoffMax     = 5 * time.Second
)

// readTimeout is how long one read waits for bytes before coming back empty-handed.
//
// A blocking read WITH A TIMEOUT, and not the 400 ms Form_Timer poll of the legacy
// application: bytes are handed over the moment they arrive instead of up to 400 ms
// later, and the loop still gets the floor back often enough to notice a cancelled
// context. One second is over twice the nominal cadence of the parc — a healthy
// scale therefore rarely times out — and a third of the three seconds a
// configuration reload allows a close to take (§11.4).
const readTimeout = time.Second

// Opener opens the port o describes and returns the byte stream to read from.
//
// It is THE seam that makes this package testable without a scale on the bench: a
// serial port cannot be opened in a test, so the reconnection, the backoff, the
// frame cut between two reads and the blocking close are all exercised through a
// stream a test hands back. nil means OpenSystemPort, the real port.
//
// CONTRACT: Read must BLOCK until bytes arrive, until its own timeout elapses, or
// until the stream is closed. A Read that returns (0, nil) at once would turn the
// reader loop into a busy loop, which is exactly what failure test 1 forbids.
type Opener func(o Options) (io.ReadCloser, error)

// Options holds the link settings of a serial scale plus the single decoder that
// varies from one model to the next.
//
// The zero value is usable once Port, Decoder and Clock are set: every other field
// falls back on the value §9.1 tabulates. That is what keeps a model package down
// to its descriptor, its defaults and its decoder (§9.3).
type Options struct {
	// Port is the device the operator declared: "COM8" on Windows, a STABLE symlink
	// such as "/dev/balance-serial" on Linux — /dev/ttyUSB0 becomes ttyUSB1 after a
	// replug, which is why §15.3 installs a udev rule rather than a device number.
	Port string
	// Baud is the bitrate, 9600 on every scale of this parc.
	Baud int
	// Bits is the character size, 5 to 8.
	Bits int
	// Parity is "N", "E" or "O", read case-insensitively.
	Parity string
	// Stop is 1 or 2 stop bits.
	Stop int
	// Decoder is THE only per-model variation point (§9.1). frame.Accumulator
	// satisfies it and its grammar covers both GRAM models.
	Decoder domain.Decoder
	// ReadBufferSize is how many bytes one read may hand back, 4096 by default.
	ReadBufferSize int
	// BackoffMin and BackoffMax bound the reconnection delay, 200 ms to 5 s. Both are
	// measured on Clock.
	BackoffMin, BackoffMax time.Duration
	// Clock is the injected clock every delay of this package is measured on. There
	// is NO default: a driver that read the real clock would make the reconnection
	// test wait seven real seconds, and there would be no such test (§5.3).
	Clock ports.Clock
	// Open opens the port. nil means OpenSystemPort, the real serial port, so that
	// the production path needs no wiring of its own.
	Open Opener
}

// OptionSchema declares the seven options of a serial link, which is what lets the
// administration screen GENERATE its form and Config.Validate check the OPTIONS of
// a driver instead of only its type name (§11.3).
//
// ReadBufferSize is deliberately absent: no operator has a legitimate choice to
// make about the size of a read (ADR-025), and the one figure that ever mattered —
// 16 bytes for 18-byte frames — was a defect, not a setting.
//
// The Values of "port" are left EMPTY, and domain.OptionSchema already reads an
// empty list as "we could not enumerate": only the platform can list the ports of
// THIS machine, and the administration screen fills them in.
func OptionSchema() []domain.OptionSchema {
	return []domain.OptionSchema{
		{Key: optionPort, Kind: domain.OptionText, Required: true},
		{Key: optionBaud, Kind: domain.OptionInt},
		{Key: optionBits, Kind: domain.OptionInt},
		{Key: optionParity, Kind: domain.OptionEnum, Values: []string{"N", "E", "O"}},
		{Key: optionStop, Kind: domain.OptionInt},
		{Key: optionBackoffMin, Kind: domain.OptionInt},
		{Key: optionBackoffMax, Kind: domain.OptionInt},
	}
}

// ParseOptions turns the driver options of config.json into link settings.
//
// It fills the LINK and nothing else: Decoder belongs to the model package, Clock
// and the technical log to the composition root. Every field a file leaves out
// falls back on the default of the parc, and Open on the real serial port.
//
// A value that is present but of the wrong type is an ERROR and never a silent
// default: `"baud": "9600"` is a type error a volunteer has to be told about, not a
// baud rate (§11.2). The returned error is FRENCH — it reaches the administration
// screen and the technical journal.
func ParseOptions(o domain.DriverOptions) (Options, error) {
	var link Options
	var err error
	if link.Port, err = optionText(o, optionPort); err != nil {
		return Options{}, err
	}
	if link.Parity, err = optionText(o, optionParity); err != nil {
		return Options{}, err
	}
	for _, field := range []struct {
		key  string
		into *int
	}{
		{optionBaud, &link.Baud},
		{optionBits, &link.Bits},
		{optionStop, &link.Stop},
	} {
		value, present, err := optionInt(o, field.key)
		if err != nil {
			return Options{}, err
		}
		if present {
			*field.into = int(value)
		}
	}
	for _, field := range []struct {
		key  string
		into *time.Duration
	}{
		{optionBackoffMin, &link.BackoffMin},
		{optionBackoffMax, &link.BackoffMax},
	} {
		value, present, err := optionInt(o, field.key)
		if err != nil {
			return Options{}, err
		}
		if !present {
			continue
		}
		if value <= 0 {
			return Options{}, fmt.Errorf("scale.options.%s : un délai doit être positif (lu %d)", field.key, value)
		}
		*field.into = time.Duration(value) * time.Millisecond
	}

	link = link.withDefaults()
	if link.BackoffMax < link.BackoffMin {
		return Options{}, fmt.Errorf("scale.options : %s (%d ms) doit être au moins égal à %s (%d ms)",
			optionBackoffMax, link.BackoffMax.Milliseconds(), optionBackoffMin, link.BackoffMin.Milliseconds())
	}
	return link, nil
}

// optionText reads a string option, telling an absent key from a key that carries
// something other than a string.
func optionText(o domain.DriverOptions, key string) (string, error) {
	if _, present := o[key]; !present {
		return "", nil
	}
	value, ok := o.Text(key)
	if !ok {
		return "", fmt.Errorf("scale.options.%s : une valeur texte est attendue", key)
	}
	return value, nil
}

// optionInt reads a whole-number option and reports whether the key was there at
// all, so that "absent" and "zero" never mean the same thing.
func optionInt(o domain.DriverOptions, key string) (int64, bool, error) {
	if _, present := o[key]; !present {
		return 0, false, nil
	}
	value, ok := o.Int(key)
	if !ok {
		return 0, false, fmt.Errorf("scale.options.%s : un nombre entier est attendu, sans guillemets", key)
	}
	return value, true, nil
}

// withDefaults fills in every field a configuration left out. It is idempotent, so
// New, Loop and ParseOptions may all call it.
func (o Options) withDefaults() Options {
	if o.Baud == 0 {
		o.Baud = defaultBaud
	}
	if o.Bits == 0 {
		o.Bits = defaultBits
	}
	if o.Parity == "" {
		o.Parity = defaultParity
	}
	if o.Stop == 0 {
		o.Stop = defaultStop
	}
	if o.ReadBufferSize == 0 {
		o.ReadBufferSize = defaultReadBufferSize
	}
	if o.BackoffMin == 0 {
		o.BackoffMin = defaultBackoffMin
	}
	if o.BackoffMax == 0 {
		o.BackoffMax = defaultBackoffMax
	}
	if o.Open == nil {
		o.Open = OpenSystemPort
	}
	return o
}

// validate reports, in French, why these options cannot be used at all.
//
// It is what makes Start fail SYNCHRONOUSLY on a fault no amount of retrying can
// fix — a port nobody declared, a decoder nobody wired, a parity that does not
// exist — instead of a driver that reconnects forever on options that will never
// work. A transient fault, on the contrary, is never validated here: a port that is
// missing right now may well be back in 200 ms, and giving up on it is the very
// defect §9.1 corrects.
func (o Options) validate() error {
	switch {
	case o.Port == "":
		return errors.New("scale.options.port : aucun port n'est déclaré")
	case o.Decoder == nil:
		return errors.New("scale.options : aucun décodeur n'est fourni")
	case o.Clock == nil:
		return errors.New("scale.options : aucune horloge n'est fournie")
	case o.Open == nil:
		return errors.New("scale.options : aucune fonction d'ouverture de port n'est fournie")
	case o.ReadBufferSize <= 0:
		return fmt.Errorf("scale.options : le tampon de lecture doit être positif (lu %d)", o.ReadBufferSize)
	case o.BackoffMin <= 0 || o.BackoffMax < o.BackoffMin:
		return fmt.Errorf("scale.options : le backoff doit croître, de %s à %s", o.BackoffMin, o.BackoffMax)
	}
	_, err := o.mode()
	return err
}

// mode maps the declared link settings onto what go.bug.st/serial expects.
func (o Options) mode() (*serialport.Mode, error) {
	mode := &serialport.Mode{BaudRate: o.Baud, DataBits: o.Bits}
	switch strings.ToUpper(o.Parity) {
	case "N":
		mode.Parity = serialport.NoParity
	case "E":
		mode.Parity = serialport.EvenParity
	case "O":
		mode.Parity = serialport.OddParity
	default:
		return nil, fmt.Errorf("scale.options.%s : la parité s'écrit N, E ou O (lu %q)", optionParity, o.Parity)
	}
	switch o.Stop {
	case 1:
		mode.StopBits = serialport.OneStopBit
	case 2:
		mode.StopBits = serialport.TwoStopBits
	default:
		return nil, fmt.Errorf("scale.options.%s : 1 ou 2 bits d'arrêt (lu %d)", optionStop, o.Stop)
	}
	if o.Bits < 5 || o.Bits > 8 {
		return nil, fmt.Errorf("scale.options.%s : de 5 à 8 bits de données (lu %d)", optionBits, o.Bits)
	}
	if o.Baud <= 0 {
		return nil, fmt.Errorf("scale.options.%s : une vitesse doit être positive (lu %d)", optionBaud, o.Baud)
	}
	return mode, nil
}

// OpenSystemPort opens a real serial port through go.bug.st/serial, the pure-Go
// module §17.1 names.
//
// The port name is handed over UNTOUCHED. That is not laziness: go.bug.st prefixes
// \\.\ itself before calling CreateFile, which is precisely what makes "COM10"
// reachable. The legacy application built the path by hand, without the prefix, and
// no station could be moved past COM9.
//
// It sets a READ TIMEOUT so that the read blocks on the bytes instead of being
// polled by a timer, and so that a cancelled context is noticed within one timeout.
func OpenSystemPort(o Options) (io.ReadCloser, error) {
	mode, err := o.mode()
	if err != nil {
		return nil, err
	}
	port, err := serialport.Open(o.Port, mode)
	if err != nil {
		return nil, err
	}
	if err := port.SetReadTimeout(readTimeout); err != nil {
		// A port whose timeout could not be set would read forever, and a Close that
		// never returns would freeze the write of a configuration (§11.4). We give it
		// back rather than keep it.
		port.Close()
		return nil, err
	}
	return port, nil
}
