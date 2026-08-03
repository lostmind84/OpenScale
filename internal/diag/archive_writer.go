package diag

import (
	"archive/zip"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// This file is how one member gets into diagnostic.zip: scrubbed unless it is binary,
// stamped on the injected clock, and — when it cannot be built at all — recorded in
// errors.txt instead of aborting the archive. It is the enforcement point of the second
// rule of archive.go, and it is deliberately the only thing in this package that touches
// the zip writer.

// memberWriter adds members to the archive, scrubbing every text one, and collects what
// went wrong instead of giving up.
type memberWriter struct {
	zip   *zip.Writer
	clean *scrubber
	clock interface{ Now() time.Time }
	notes []string
}

// text adds one text member, scrubbed.
func (m *memberWriter) text(name, content string) {
	m.raw(name, []byte(m.clean.Clean(content)))
}

// bytes adds one text member that is already a byte slice, scrubbed.
func (m *memberWriter) bytes(name string, content []byte) {
	m.raw(name, m.clean.CleanBytes(content))
}

// json adds one member as indented JSON, scrubbed.
func (m *memberWriter) json(name string, value any) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		m.fail(name, err)
		return
	}
	m.bytes(name, raw)
}

// csv adds one member as a semicolon-separated CSV with a UTF-8 BOM, scrubbed.
//
// A semicolon and a BOM for the reason internal/web already gives: this file is opened in
// the spreadsheet of a French Windows, where a comma-separated file lands in one column. It
// is the same trade-off the producer's own export makes (§10.2).
func (m *memberWriter) csv(name string, header []string, rows [][]string) {
	out := &strings.Builder{}
	out.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(out)
	writer.Comma = ';'
	_ = writer.Write(header)
	for _, row := range rows {
		_ = writer.Write(row)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		m.fail(name, err)
		return
	}
	m.text(name, out.String())
}

// raw adds one member verbatim, WITHOUT scrubbing. It is for binary content only.
func (m *memberWriter) raw(name string, content []byte) {
	entry, err := m.zip.CreateHeader(&zip.FileHeader{
		Name:     name,
		Method:   zip.Deflate,
		Modified: m.now(),
	})
	if err != nil {
		m.note(name, "membre non créé : "+err.Error())
		return
	}
	if _, err := entry.Write(content); err != nil {
		m.note(name, "membre incomplet : "+err.Error())
	}
}

// fail records that one member could not be built, and writes the reason where the reader
// will find it.
func (m *memberWriter) fail(name string, err error) {
	m.note(name, err.Error())
}

// note records one line for errors.txt.
func (m *memberWriter) note(name, message string) {
	m.notes = append(m.notes, name+" : "+message)
}

// errorsMember writes what could not be gathered.
//
// It is written EVEN WHEN EMPTY, and that is the point: a reader who finds errors.txt saying
// « rien à signaler » knows the archive is complete, whereas a missing file could mean
// either « nothing failed » or « the archive was truncated ».
func (m *memberWriter) errorsMember() {
	out := &strings.Builder{}
	fmt.Fprintf(out, "# Ce qui n'a pas pu être rassemblé dans cette archive.\n")
	fmt.Fprintf(out, "# Une archive incomplète reste utile : c'est justement les matins où quelque\n")
	fmt.Fprintf(out, "# chose est cassé qu'on appuie sur ce bouton.\n\n")
	if len(m.notes) == 0 {
		fmt.Fprintf(out, "rien à signaler : tous les membres ont été écrits.\n")
	}
	for _, note := range m.notes {
		fmt.Fprintf(out, "%s\n", note)
	}
	// Written through raw and scrubbed by hand: adding a member from inside the member
	// writer must not be able to append to m.notes while it is being rendered.
	m.raw("errors.txt", m.clean.CleanBytes([]byte(out.String())))
}

// now is the instant stamped on every member, read from the INJECTED clock.
//
// Every member carries the SAME instant, which is what makes an archive reproducible in a
// test: a member stamped from the wall clock would make two archives of one frozen station
// differ.
func (m *memberWriter) now() time.Time {
	if m.clock == nil {
		return time.Time{}
	}
	return m.clock.Now()
}
