package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// watermarkFile is the name, under <dataDir>/catalog, of the file holding the stamp.
const watermarkFile = "watermark.json"

// Watermark remembers the stamp of the last catalog file a source acknowledged without
// deleting it, which is what `after_read: keep` has to offer in place of the removal.
//
// Without it a kept file would be read again at every poll, and again after every
// restart. The memory is on disk for the second reason: a station that reboots every
// morning must not re-apply an unchanged catalog each time.
//
// It is not safe for concurrent use. It is meant to be called from the single goroutine
// that runs a source's Next and Acknowledge (internal/station/catalogwatch.go), never
// from Close or Wake.
type Watermark struct {
	path     string
	location string
	stamp    Stamp
	held     bool
	// content is the sha256 of the refused content left in place, empty when none.
	content string
}

// watermarkRecord is the on-disk form. The location is written with the stamp so that
// pointing the source at another file never inherits the memory of the previous one.
type watermarkRecord struct {
	Location string    `json:"location"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	// Content is the sha256 of a refused catalog left in place. It is what lets a share
	// that dates nothing, and so never gives the stamp a chance to match, avoid
	// quarantining the same refused file again at every stable poll.
	Content string `json:"content,omitempty"`
}

// OpenWatermark returns the watermark of a location, empty when nothing usable is held.
//
// A missing file, an unreadable one, or one written for another location all give an
// empty watermark and never an error: the worst consequence is one extra read of the
// catalog, which the transaction downstream makes harmless, whereas refusing to start
// over a damaged bookkeeping file would stop the catalog from ever being refreshed.
func OpenWatermark(dataDir, location string) (*Watermark, error) {
	watermark := &Watermark{path: filepath.Join(dataDir, "catalog", watermarkFile), location: location}
	raw, err := os.ReadFile(watermark.path)
	if err != nil {
		return watermark, nil
	}
	var record watermarkRecord
	if err := json.Unmarshal(raw, &record); err != nil || record.Location != location {
		return watermark, nil
	}
	watermark.stamp = Stamp{Size: record.Size, Modified: record.Modified}
	watermark.held = true
	watermark.content = record.Content
	return watermark, nil
}

// Seen reports whether the file described by s is the one already acknowledged.
//
// A stamp without a date never matches: a source that cannot tell when a file was
// written (a server that omits the header) has no way to notice that it changed, so
// the size alone must not silence it.
func (w *Watermark) Seen(s Stamp) bool {
	if !w.held || s.Modified.IsZero() {
		return false
	}
	return s.Size == w.stamp.Size && s.Modified.Equal(w.stamp.Modified)
}

// Remember retains s and writes it to disk.
//
// The write goes through a temporary file of the same directory, synced then renamed,
// so that a power cut leaves either the previous watermark or the new one, never half
// of a JSON document that would be read back as empty.
//
// The stamp is retained in memory BEFORE the write, and stays retained when the write
// fails: the error is returned for the caller to report, but an unwritable disk must
// not make the same unchanged (or refused) file be read, applied or quarantined again
// at every stable poll. The price is that a restart will read it once more.
//
// A stamp acknowledges a file that was APPLIED or whose refusal is already counted by
// its date: the refused content RememberRefusal held, if any, is released.
func (w *Watermark) Remember(s Stamp) error {
	w.stamp, w.held, w.content = s, true, ""
	return w.persist()
}

// RememberRefusal retains the stamp of a refused file AND the sha256 of its content, and
// writes them to disk in ONE write.
//
// It is the second line of defence of a refused file left in place: the stamp stops it
// being read again, but a stamp without a date never matches (see Seen), and the same
// broken content would then be archived and counted at every stable poll until the
// quarantine bans it — which is the red light a file merely LEFT in place must not turn
// on. Both values are set in memory before the write and stay set when it fails, so an
// unwritable disk cannot bring the repetition back; they are one call, and not Remember
// followed by another, because Remember releases the content and a failed write between
// the two would leave it released.
func (w *Watermark) RememberRefusal(s Stamp, sha string) error {
	w.stamp, w.held, w.content = s, true, sha
	return w.persist()
}

// HoldsContent reports whether sha is the refused content already retained. An empty
// digest is never held.
func (w *Watermark) HoldsContent(sha string) bool {
	return sha != "" && w.content == sha
}

// persist writes the current state to disk, atomically.
func (w *Watermark) persist() error {
	directory := filepath.Dir(w.path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("création du dossier du repère : %w", err)
	}
	raw, err := json.Marshal(watermarkRecord{
		Location: w.location, Size: w.stamp.Size, Modified: w.stamp.Modified, Content: w.content})
	if err != nil {
		return fmt.Errorf("encodage du repère : %w", err)
	}
	temporary, err := os.CreateTemp(directory, watermarkFile+".*.part")
	if err != nil {
		return fmt.Errorf("écriture du repère : %w", err)
	}
	name := temporary.Name()
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		_ = os.Remove(name)
		return fmt.Errorf("écriture du repère : %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		_ = os.Remove(name)
		return fmt.Errorf("écriture du repère : %w", err)
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("fermeture du repère : %w", err)
	}
	if err := os.Rename(name, w.path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("nommage du repère : %w", err)
	}
	return nil
}

// Forget drops the stamp and the held content from memory and removes the record from
// disk.
//
// It is what a source calls when `after_read` goes back to deleting: a stamp left behind
// would be reloaded the day the mode returns to keep, and would silently skip a NEW file
// that happens to share the size and the date of the old one. The record is removed
// WHATEVER location it holds: a station has one catalog source, so a record written for
// another location is the memory of a catalog no longer in service, and keep(A) then
// delete(B) then keep(A) must not bring A's stamp back. A record already absent is not an
// error, and the memory is cleared even when the removal fails.
func (w *Watermark) Forget() error {
	w.stamp, w.held, w.content = Stamp{}, false, ""
	if err := os.Remove(w.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("effacement du repère : %w", err)
	}
	return nil
}
