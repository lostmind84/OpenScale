package catalog

import (
	"encoding/json"
	"fmt"
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
type Watermark struct {
	path     string
	location string
	stamp    Stamp
	held     bool
}

// watermarkRecord is the on-disk form. The location is written with the stamp so that
// pointing the source at another file never inherits the memory of the previous one.
type watermarkRecord struct {
	Location string    `json:"location"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
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
func (w *Watermark) Remember(s Stamp) error {
	directory := filepath.Dir(w.path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("création du dossier du repère : %w", err)
	}
	raw, err := json.Marshal(watermarkRecord{Location: w.location, Size: s.Size, Modified: s.Modified})
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
	w.stamp, w.held = s, true
	return nil
}
