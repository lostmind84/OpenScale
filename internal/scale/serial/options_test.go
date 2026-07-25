package serial

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	serialport "go.bug.st/serial"

	"openscale/internal/domain"
	"openscale/internal/domain/frame"
)

// deliveredConfigPath is the file lot L9 ships and the installer copies. It is read
// rather than paraphrased: the delivered configuration is what the parser must be
// able to read back, and a test that retyped it would prove nothing about it.
var deliveredConfigPath = filepath.Join("..", "..", "..", "testdata", "config-lacagette.json")

// deliveredScaleOptions returns the scale.options block of the delivered file.
func deliveredScaleOptions(t *testing.T) domain.DriverOptions {
	t.Helper()
	raw, err := os.ReadFile(deliveredConfigPath)
	if err != nil {
		t.Fatalf("lecture de %s : %v", deliveredConfigPath, err)
	}
	var file struct {
		Scale struct {
			Options domain.DriverOptions `json:"options"`
		} `json:"scale"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("décodage de %s : %v", deliveredConfigPath, err)
	}
	if len(file.Scale.Options) == 0 {
		t.Fatalf("%s ne porte aucune option de balance", deliveredConfigPath)
	}
	return file.Scale.Options
}

// linkSettings is the comparable half of Options: everything a configuration file
// carries, and nothing that is a function or an interface. Options itself holds an
// Opener, so it cannot be compared with ==.
type linkSettings struct {
	Port                   string
	Baud, Bits             int
	Parity                 string
	Stop                   int
	ReadBufferSize         int
	BackoffMin, BackoffMax time.Duration
}

// linkOf keeps of an Options only what a file declared.
func linkOf(o Options) linkSettings {
	return linkSettings{
		Port: o.Port, Baud: o.Baud, Bits: o.Bits, Parity: o.Parity, Stop: o.Stop,
		ReadBufferSize: o.ReadBufferSize, BackoffMin: o.BackoffMin, BackoffMax: o.BackoffMax,
	}
}

// driverOptions builds an options block the way a file carries it.
func driverOptions(t *testing.T, pairs map[string]any) domain.DriverOptions {
	t.Helper()
	out := domain.DriverOptions{}
	for key, value := range pairs {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("encodage de %s : %v", key, err)
		}
		out[key] = raw
	}
	return out
}

func TestParseOptionsReadsTheDeliveredConfiguration(t *testing.T) {
	link, err := ParseOptions(deliveredScaleOptions(t))
	if err != nil {
		t.Fatalf("ParseOptions : %v", err)
	}
	want := linkSettings{
		Port: "COM8", Baud: 9600, Bits: 8, Parity: "N", Stop: 1,
		ReadBufferSize: defaultReadBufferSize,
		BackoffMin:     200 * time.Millisecond,
		BackoffMax:     5 * time.Second,
	}
	if got := linkOf(link); got != want {
		t.Errorf("liaison %+v, attendu %+v", got, want)
	}
	if link.Open == nil {
		t.Error("Open nil : sans réglage explicite c'est le vrai port série qui est visé")
	}
}

func TestOptionSchemaDeclaresExactlyTheKeysOfTheDeliveredConfiguration(t *testing.T) {
	// The form the administration screen generates and the file a station carries are
	// two halves of one contract: a key in the file that the schema ignores is a setting
	// nobody can see, and a key in the schema that no file uses is a field nobody fills.
	declared := map[string]bool{}
	for _, option := range OptionSchema() {
		declared[option.Key] = true
	}
	for key := range deliveredScaleOptions(t) {
		if !declared[key] {
			t.Errorf("la configuration livrée porte %q, absent du schéma", key)
		}
	}
	for key := range declared {
		if _, present := deliveredScaleOptions(t)[key]; !present {
			t.Errorf("le schéma déclare %q, que la configuration livrée n'utilise pas", key)
		}
	}
	if port := OptionSchema()[0]; port.Key != optionPort || !port.Required {
		t.Errorf("première option %+v, attendu un port obligatoire", port)
	}
	if values := OptionSchema()[0].Values; len(values) != 0 {
		t.Errorf("valeurs du port %v : seule la plateforme énumère les ports de CETTE "+
			"machine, et une liste vide se lit « on n'a pas pu énumérer »", values)
	}
}

func TestParseOptionsFillsTheDefaultsOfTheParc(t *testing.T) {
	link, err := ParseOptions(driverOptions(t, map[string]any{"port": "/dev/balance-serial"}))
	if err != nil {
		t.Fatalf("ParseOptions : %v", err)
	}
	if link.Baud != 9600 || link.Bits != 8 || link.Parity != "N" || link.Stop != 1 {
		t.Errorf("liaison %d %d%s%d, attendu 9600 8N1", link.Baud, link.Bits, link.Parity, link.Stop)
	}
	if link.ReadBufferSize != 4096 {
		t.Errorf("tampon de %d octets, attendu 4096", link.ReadBufferSize)
	}
	if link.BackoffMin != 200*time.Millisecond || link.BackoffMax != 5*time.Second {
		t.Errorf("backoff %v → %v, attendu 200 ms → 5 s", link.BackoffMin, link.BackoffMax)
	}
}

func TestParseOptionsRefusesWhatItCannotRead(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options map[string]any
		names   string // the key the message must name
	}{
		{"a baud rate spelled as text", map[string]any{"port": "COM8", "baud": "9600"}, "baud"},
		{"a port that is not a string", map[string]any{"port": 8}, "port"},
		{"a parity that is a number", map[string]any{"port": "COM8", "parity": 1}, "parity"},
		{"a fractional number of bits", map[string]any{"port": "COM8", "bits": 7.5}, "bits"},
		{"a negative backoff", map[string]any{"port": "COM8", "backoff_min_ms": -1}, "backoff_min_ms"},
		{"a backoff spelled as text", map[string]any{
			"port": "COM8", "backoff_max_ms": "5000"}, "backoff_max_ms"},
		{"a backoff that shrinks", map[string]any{
			"port": "COM8", "backoff_min_ms": 5000, "backoff_max_ms": 200}, "backoff_max_ms"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseOptions(driverOptions(t, tc.options))
			if err == nil {
				t.Fatalf("options %v acceptées", tc.options)
			}
			if !strings.Contains(err.Error(), tc.names) {
				t.Errorf("message %q, il doit nommer %q — un bénévole doit savoir "+
					"quel champ corriger", err, tc.names)
			}
		})
	}
}

func TestValidateRefusesOnlyWhatNoRetryCouldFix(t *testing.T) {
	complete := func() Options {
		return Options{
			Port: "COM8", Decoder: &frame.Accumulator{}, Clock: newRecordingClock(),
		}.withDefaults()
	}
	if err := complete().validate(); err != nil {
		t.Fatalf("des options complètes sont refusées : %v", err)
	}

	for _, tc := range []struct {
		name   string
		damage func(*Options)
		names  string
	}{
		{"no port at all", func(o *Options) { o.Port = "" }, "port"},
		{"no decoder", func(o *Options) { o.Decoder = nil }, "décodeur"},
		{"no clock", func(o *Options) { o.Clock = nil }, "horloge"},
		{"no opener", func(o *Options) { o.Open = nil }, "ouverture"},
		{"an empty read buffer", func(o *Options) { o.ReadBufferSize = -1 }, "tampon"},
		{"a backoff that shrinks", func(o *Options) { o.BackoffMax = time.Millisecond }, "backoff"},
		{"a parity that does not exist", func(o *Options) { o.Parity = "K" }, "parité"},
		{"one stop bit and a half", func(o *Options) { o.Stop = 3 }, "arrêt"},
		{"four data bits", func(o *Options) { o.Bits = 4 }, "bits de données"},
		{"a baud rate of zero", func(o *Options) { o.Baud = -1 }, "vitesse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := complete()
			tc.damage(&options)
			err := options.validate()
			if err == nil {
				t.Fatal("options acceptées")
			}
			if !strings.Contains(err.Error(), tc.names) {
				t.Errorf("message %q, il doit nommer %q", err, tc.names)
			}
		})
	}
}

func TestModeMapsTheLinkSettings(t *testing.T) {
	for _, tc := range []struct {
		parity string
		want   serialport.Parity
	}{
		{"N", serialport.NoParity},
		{"n", serialport.NoParity}, // read case-insensitively, like the frame grammar
		{"E", serialport.EvenParity},
		{"e", serialport.EvenParity},
		{"O", serialport.OddParity},
		{"o", serialport.OddParity},
	} {
		options := Options{Port: "COM8", Parity: tc.parity}.withDefaults()
		mode, err := options.mode()
		if err != nil {
			t.Fatalf("parité %q : %v", tc.parity, err)
		}
		if mode.Parity != tc.want {
			t.Errorf("parité %q → %v, attendu %v", tc.parity, mode.Parity, tc.want)
		}
	}

	for stop, want := range map[int]serialport.StopBits{
		1: serialport.OneStopBit,
		2: serialport.TwoStopBits,
	} {
		options := Options{Port: "COM8", Stop: stop}.withDefaults()
		mode, err := options.mode()
		if err != nil {
			t.Fatalf("%d bit(s) d'arrêt : %v", stop, err)
		}
		if mode.StopBits != want {
			t.Errorf("%d bit(s) d'arrêt → %v, attendu %v", stop, mode.StopBits, want)
		}
	}

	mode, err := Options{Port: "COM8", Baud: 19200, Bits: 7}.withDefaults().mode()
	if err != nil {
		t.Fatalf("mode : %v", err)
	}
	if mode.BaudRate != 19200 || mode.DataBits != 7 {
		t.Errorf("mode %d %d, attendu 19200 7", mode.BaudRate, mode.DataBits)
	}
}

func TestOpenSystemPortRefusesBeforeItTouchesTheHardware(t *testing.T) {
	// A parity that does not exist is settled without opening anything: the only branch
	// of the real opener a test can reach, and the one that matters, because a
	// misconfigured link must fail with a sentence and not with "Access denied".
	if _, err := OpenSystemPort(Options{Port: "COM8", Parity: "K", Stop: 1, Bits: 8, Baud: 9600}); err == nil {
		t.Error("une parité inexistante a été acceptée")
	}
	// And a port name no machine can carry fails as a device error, without hanging.
	if _, err := OpenSystemPort(Options{}.withDefaults()); err == nil {
		t.Error("un port sans nom a été ouvert")
	}
	if _, err := OpenSystemPort(Options{Port: "OPENSCALE-NO-SUCH-PORT"}.withDefaults()); err == nil {
		t.Error("un port inexistant a été ouvert")
	}
}

func TestWithDefaultsIsIdempotent(t *testing.T) {
	// New, Loop and ParseOptions all call it, and a second pass must not move a value a
	// station really declared.
	declared := Options{
		Port: "COM8", Baud: 19200, Bits: 7, Parity: "E", Stop: 2,
		ReadBufferSize: 64, BackoffMin: time.Second, BackoffMax: 2 * time.Second,
	}
	once := declared.withDefaults()
	twice := once.withDefaults()
	if linkOf(once) != linkOf(twice) {
		t.Errorf("second passage %+v, attendu %+v", linkOf(twice), linkOf(once))
	}
	if once.Baud != 19200 || once.Bits != 7 || once.Parity != "E" || once.Stop != 2 ||
		once.ReadBufferSize != 64 || once.BackoffMin != time.Second {
		t.Errorf("les valeurs déclarées ont été écrasées : %+v", once)
	}
}
