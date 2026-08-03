package diag

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"strings"
)

// This file reads what the machine does with its own power: the settings §15.2 step 5
// turns off, and the right to restart the computer at all. They belong together because
// both are invisible until the morning they cost something — a USB adapter suspended out
// from under the scale, or a « Redémarrer l'ordinateur » button that answers « accès
// refusé » to a volunteer standing in front of a frozen kiosk.

// The two GUIDs of §15.2, step 5, copied from install.ps1.
//
// They are NOT derived, NOT guessed and NOT looked up: the document contains the exact
// line `powercfg /setacvalueindex SCHEME_CURRENT 2a737441-… 48e6b7a6-… 0`, and these are
// its two arguments. The subgroup is the USB settings, the setting is the selective
// suspend — which §15.2 says causes half the « la balance ne répond plus » on a USB-serial
// adapter.
const (
	usbSubgroupGUID = "2a737441-1930-4402-8d77-b2bebba308a3"
	usbSuspendGUID  = "48e6b7a6-50f5-4782-a5d4-53bb8f07e226"
)

// powerSetting is one setting §15.2 step 5 turns off, with the sentence that names it.
type powerSetting struct {
	// subgroup and setting are powercfg arguments: either its own documented aliases, or
	// the two GUIDs §15.2 spells out.
	subgroup string
	setting  string
	// label is FRENCH and names the setting the way a volunteer would recognise it in the
	// power plan window.
	label string
}

// sleepSettings are the three timeouts §15.2 sets to zero with `powercfg /change`.
//
// The arguments are powercfg's OWN aliases and not GUIDs: SUB_SLEEP, STANDBYIDLE,
// SUB_VIDEO, VIDEOIDLE and HIBERNATEIDLE are names the tool accepts and prints, so nothing
// here is a number this project had to find somewhere.
var sleepSettings = []powerSetting{
	{"SUB_SLEEP", "STANDBYIDLE", "mise en veille"},
	{"SUB_SLEEP", "HIBERNATEIDLE", "mise en veille prolongée"},
	{"SUB_VIDEO", "VIDEOIDLE", "extinction de l'écran"},
}

// Power reports the sleep and USB selective suspend settings.
func (m hostMachine) Power(ctx context.Context) (PowerState, error) {
	if runtime.GOOS != "windows" {
		// §15.3 installs cage, seatd and udev rules, and writes no power setting at all.
		// Reporting a verdict about a Linux power plan the installer never touches would
		// be inventing a requirement.
		return PowerState{Applicable: false}, nil
	}
	state := PowerState{Applicable: true, Determined: true,
		SleepDisabled: true, USBSelectiveSuspendDisabled: true}
	var awake []string

	for _, setting := range append(sleepSettings,
		powerSetting{usbSubgroupGUID, usbSuspendGUID, "suspension USB sélective"}) {
		out, err := m.run.Run(ctx, "powercfg.exe", "/query", "SCHEME_CURRENT", setting.subgroup, setting.setting)
		value, ok := parsePowerIndex(out)
		if err != nil || !ok {
			state.Determined = false
			state.Detail = fmt.Sprintf("le réglage « %s » n'a pas pu être lu", setting.label)
			return state, nil
		}
		if value == 0 {
			continue
		}
		awake = append(awake, fmt.Sprintf("%s : %d", setting.label, value))
		if setting.setting == usbSuspendGUID {
			state.USBSelectiveSuspendDisabled = false
			continue
		}
		state.SleepDisabled = false
	}
	if len(awake) > 0 {
		state.Detail = "Réglages encore actifs sur secteur — " + strings.Join(awake, " · ") + "."
	}
	return state, nil
}

// parsePowerIndex reads the ON-MAINS setting index out of `powercfg /query` output.
//
// # Why it reads no label
//
// powercfg localises every label it prints — « Current AC Power Setting Index » becomes
// « Index du paramètre d'alimentation sur secteur actuel » on a French Windows — so a parser
// that matched on words would work on the developer's machine and fail on every station in
// the shop. What is NOT localised is the shape: the values are hexadecimal, spelled 0x…, and
// the two CURRENT indices are the last two lines of the block, mains first.
//
// # Why the last two and not the first
//
// A range setting (the sleep timeouts) prints its bounds first — minimum, maximum,
// increment — and only then the two current indices; an enumerated setting (the USB
// selective suspend) prints its possible values with UNPREFIXED indices, so they are not
// picked up at all. Taking the first 0x value would read the minimum of a range and report
// every station's sleep timeout as zero, which is the wrong answer in the dangerous
// direction: it would announce « veille désactivée » on a station that falls asleep.
//
// A block with a single value is a setting that has no battery variant, and that value is
// the mains one.
func parsePowerIndex(output string) (uint64, bool) {
	var values []uint64
	for _, line := range strings.Split(output, "\n") {
		for _, field := range strings.Fields(line) {
			hex, found := strings.CutPrefix(strings.ToLower(field), "0x")
			if !found {
				continue
			}
			value, err := strconv.ParseUint(hex, 16, 64)
			if err != nil {
				continue
			}
			values = append(values, value)
		}
	}
	switch len(values) {
	case 0:
		return 0, false
	case 1:
		return values[0], true
	}
	return values[len(values)-2], true
}

// RebootPermission reports whether this station may restart the machine.
//
// The three answers are three platforms, and the middle one is why this question exists:
// under Linux the service runs as `openscale` and polkit stands between it and the right,
// so a station missing its rule works perfectly — right up to the evening a volunteer is
// facing a frozen kiosk and touches the one button that would have saved them.
func (hostMachine) RebootPermission(context.Context) (RebootPermissionState, error) {
	allowed, detail := rebootPermission()
	if detail == "" {
		return RebootPermissionState{Applicable: false}, nil
	}
	return RebootPermissionState{Allowed: allowed, Detail: detail, Applicable: true}, nil
}
