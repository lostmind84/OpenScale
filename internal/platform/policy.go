package platform

// PolicyKind is the registry type one policy value takes.
//
// A Chromium policy has ONE documented type, and the browser silently ignores a value
// written in the other: a URLAllowlist entry stored as a number is a door left open with
// nothing in any log to say so. That is the whole reason this is carried explicitly
// instead of being guessed from the data.
type PolicyKind int

const (
	// PolicyString is REG_SZ — an address, a URL pattern.
	PolicyString PolicyKind = iota
	// PolicyDWord is REG_DWORD, which is what a Chromium policy takes for both its
	// booleans and its enumerations.
	PolicyDWord
)

// PolicyValue is one entry of a browser policy, as a registry hive stores it.
//
// It exists because a Chromium browser is configured by REGISTRY VALUES and not by a
// command line: the switches of §15.2 shape the window, and nothing on a command line can
// say « this browser may open one address and no other ». That sentence is a policy, and a
// policy is a value under a key.
type PolicyValue struct {
	// Key is the path UNDER the policy root: empty for a value at the root,
	// « URLAllowlist » for one entry of a list, which Chromium reads as a subkey whose
	// values are named "1", "2"…
	Key string
	// Name is the value name — « 1 » inside a list, « DeveloperToolsAvailability » at the
	// root.
	Name string
	// Text is the data when Kind is PolicyString.
	Text string
	// Number is the data when Kind is PolicyDWord.
	Number uint32
	Kind   PolicyKind
}

// PolicyText builds a REG_SZ value.
func PolicyText(key, name, text string) PolicyValue {
	return PolicyValue{Key: key, Name: name, Text: text, Kind: PolicyString}
}

// PolicyNumber builds a REG_DWORD value.
func PolicyNumber(key, name string, number uint32) PolicyValue {
	return PolicyValue{Key: key, Name: name, Number: number, Kind: PolicyDWord}
}

// WriteUserPolicies writes values under the CURRENT USER's policy root and returns how
// many it wrote.
//
// # Why the current user and not the machine
//
// A machine-wide policy binds every account of the PC, including the one a volunteer or a
// technician logs onto to look something up. The station account is dedicated (§15.2 step
// 1) and it is the only one that must be unable to leave the client screen, so the policy
// belongs to ITS hive and nowhere else.
//
// That choice is also what lets this be written by the KIOSK PROCESS rather than by the
// installer: `openscale kiosk` runs as the station account, in its own session, before it
// launches the browser. The installer would have to load a hive that does not exist yet —
// New-LocalUser creates an account, not a profile — and would then write once, where this
// writes at every logon and repairs a key somebody deleted.
//
// # What it is worth
//
// A policy the browser ignores is a policy nobody notices. That is why it is a BELT and
// not the guarantee: the supervisor's watch over the attached client screen brings the
// station back whatever the browser did with these values.
//
// root is the policy path of one browser, relative to the hive —
// « Software\Policies\Microsoft\Edge ».
func WriteUserPolicies(root string, values []PolicyValue) (int, error) {
	return writeUserPolicies(root, values)
}
