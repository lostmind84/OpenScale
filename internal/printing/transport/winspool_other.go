//go:build !windows

package transport

import "errors"

// This is the twin §5.1 calls `_other.go`: the platform this binary was built for has no
// Windows spooler.
//
// It REFUSES at the one place that cannot work, and it compiles everywhere else. That
// choice is not cosmetic. A build tag on the whole transport would make
// `printer.options.transport = "winspool"` a COMPILATION question — the value would
// simply not exist in a Linux binary, Config.Validate would call it « transport
// inconnu », and a station cloned from a Windows configuration (§11.5) would fail
// validation with a message about a name rather than about a platform. Here the name
// exists on the three targets, the configuration validates, and what a volunteer reads
// is the truth: this transport needs Windows.

// OpenSystemQueue refuses, in French, on every platform that has no Windows spooler.
//
// The Linux answer is NOT CUPS. §8.1 and §19 both put the CUPS/IPP path outside V1 — it
// is worth ~280 lines whose only use is driving a NON-SATO printer, a case the parc does
// not contain — and §8.4 gives Linux its own default instead: the print node of the
// system, which is the devfile transport.
func OpenSystemQueue(string) (Sink, error) {
	return nil, errors.New("la file d'impression Windows n'existe que sous Windows ; " +
		"sur ce système, le transport local est « devfile », le nœud d'impression " +
		"(/dev/usb/lp0 ou le lien udev qui le nomme)")
}
