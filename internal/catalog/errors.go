package catalog

import "errors"

// The two catalog failures, and they are SEPARATE on purpose (important-12).
//
// One is about the content of a file and quarantines it after three attempts; the
// other is about a file that was read and applied and could not be deleted, and it
// quarantines nothing, ever. A red light that fires wrongly is the worst enemy of
// operations: after three false alarms the team stops looking at the lights (§10.5).
var (
	// ErrContent reports a file whose CONTENT cannot become a catalog: it does not
	// parse, or too few of its lines are products at all. Code ERR-CAT-03, red light
	// after three failures, quarantined.
	ErrContent = errors.New("catalogue : le contenu du fichier n'est pas exploitable")
	// ErrNotAcknowledged reports a file that was read and applied but could not be
	// removed — a read-only directory, missing rights. Code ERR-CAT-05, amber, and it
	// NEVER increments the quarantine: the catalog it carried is in service.
	ErrNotAcknowledged = errors.New("catalogue : le fichier n'a pas pu être supprimé après lecture")
)

// ContentError is a refused file that says WHICH CONTENT was refused.
//
// The sha is the whole point of the type. §10.5 indexes the quarantine by sha256 and
// bans a content after three failures, so a refusal that could not name the content it
// refused would count three attempts against three unknowns and never reach the
// threshold. The sha is computed as the bytes go by, so it is available on every
// refusal — including the one that has no product at all — and it covers exactly what
// was read: on a file past the ceiling of §10.1 that is the ceiling's worth of bytes,
// which still identifies the file that keeps coming back.
type ContentError struct {
	// SHA256 is the hexadecimal digest of what was read, and the key of the quarantine.
	SHA256 string
	// Bytes is how much went past, which the import record calls its byte count.
	Bytes int64
	// Err is the refusal itself, and it always wraps ErrContent.
	Err error
}

// Error reports the refusal, in the French the archived .reason.txt carries.
func (e *ContentError) Error() string { return e.Err.Error() }

// Unwrap yields the refusal, so that errors.Is reaches ErrContent through it.
func (e *ContentError) Unwrap() error { return e.Err }

// Refused wraps a refusal with the content it bears on.
//
// A helper rather than a struct literal at each of the parser's exits: the sha and the
// byte count are read from the same two values in every one of them, and a refusal
// that forgot one of them would be a refusal nobody could count.
func Refused(sha string, bytes int64, cause error) error {
	return &ContentError{SHA256: sha, Bytes: bytes, Err: cause}
}

// ContentOf reports the content a refusal bears on, and whether it names one.
//
// It is what tells a CONTENT failure from every other kind at the one place where the
// distinction has teeth — the quarantine counter of §10.5 — without anybody having to
// remember the rule.
func ContentOf(err error) (*ContentError, bool) {
	var content *ContentError
	if errors.As(err, &content) {
		return content, true
	}
	return nil, false
}
