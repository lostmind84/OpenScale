package catalog

import (
	"context"

	"openscale/internal/domain"
)

// DefaultFailuresBeforeReject is the value catalog.options ships with (§11.2).
//
// It is used when the key is absent, and the reason it is not zero is the reason every
// other default in this package is not zero: a station whose configuration lost a line
// must keep the guard, not lose it.
const DefaultFailuresBeforeReject = 3

// FailureLedger is the part of the station's database the quarantine needs, and it is
// declared HERE, on the CONSUMER's side (cut 3 of §5.2).
//
// Two calls: count one failure, read what is already counted. internal/store satisfies
// it as it stands, and this package therefore links no SQL driver into a watcher of a
// directory.
type FailureLedger interface {
	// RecordContentFailure counts one CONTENT failure and reports the resulting state.
	RecordContentFailure(ctx context.Context, sha, code, reason string) (domain.QuarantineEntry, error)
	// Quarantine reports what already stands against a content, or an error when
	// nothing does.
	Quarantine(ctx context.Context, sha string) (domain.QuarantineEntry, error)
}

// Quarantine is the rule of §10.5: three refusals of the SAME CONTENT and the file is
// refused outright instead of being tried again every five seconds.
//
// It counts by sha256 and never by file name, because the name is the same every night
// and the content is what fails. And it counts CONTENT failures only: a file that was
// read and applied and could not be deleted is ERR-CAT-05, amber, and it must never
// reach this counter — after three false alarms the team stops looking at the lights.
type Quarantine struct {
	ledger FailureLedger
	// threshold is catalog.options.failures_before_reject.
	threshold int
}

// NewQuarantine returns the rule for a ledger and a threshold.
//
// A nil ledger is legitimate and makes every method a no-op: a source that is being
// exercised without a database still has to be able to refuse a file, and the refusal
// itself — archive, reason, removal — owes nothing to the counter.
func NewQuarantine(ledger FailureLedger, threshold int) *Quarantine {
	if threshold < 1 {
		threshold = DefaultFailuresBeforeReject
	}
	return &Quarantine{ledger: ledger, threshold: threshold}
}

// Threshold reports how many failures ban a content, which the administration screen
// shows next to the count of the day.
func (q *Quarantine) Threshold() int {
	if q == nil {
		return DefaultFailuresBeforeReject
	}
	return q.threshold
}

// Count records one failure against the content that caused err, and reports the state
// it produced.
//
// IT TAKES THE ERROR AND NOT A SHA, and that is the whole safety of this type: only a
// *ContentError carries a sha, and only a content failure produces one. An
// acknowledgement that failed cannot be counted here even by mistake — which is
// exactly the trap of §16.2 line 11, where a read-only directory must leave the
// quarantine untouched.
//
// counted is false when nothing was counted: no ledger, or a failure that is not about
// a content. The caller then knows its refusal is not on the way to a red light.
func (q *Quarantine) Count(ctx context.Context, err error) (entry domain.QuarantineEntry, counted bool) {
	if q == nil || q.ledger == nil {
		return domain.QuarantineEntry{}, false
	}
	content, ok := ContentOf(err)
	if !ok || content.SHA256 == "" {
		return domain.QuarantineEntry{}, false
	}
	recorded, recordErr := q.ledger.RecordContentFailure(ctx, content.SHA256, "ERR-CAT-03", err.Error())
	if recordErr != nil {
		return domain.QuarantineEntry{}, false
	}
	return recorded, true
}

// Banned reports whether a content has already failed often enough to be refused
// without being examined again.
//
// The comparison is `>=` and not `>`: failures_before_reject is « le rejet immédiat ne
// s'applique que si failure_count >= failures_before_reject », and the shipped value is
// three (§10.5).
func (q *Quarantine) Banned(ctx context.Context, sha string) (domain.QuarantineEntry, bool) {
	if q == nil || q.ledger == nil || sha == "" {
		return domain.QuarantineEntry{}, false
	}
	entry, err := q.ledger.Quarantine(ctx, sha)
	if err != nil {
		// Unknown content is the answer for every file a station has ever accepted, and
		// it is not a failure of anything.
		return domain.QuarantineEntry{}, false
	}
	return entry, entry.FailureCount >= q.threshold
}
