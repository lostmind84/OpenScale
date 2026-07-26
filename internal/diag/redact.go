package diag

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"openscale/internal/domain"
)

// THIS FILE IS A SECURITY REQUIREMENT, not a courtesy.
//
// diagnostic.zip leaves the shop. §15.4 gives it « un seul bouton, sans mot de passe »
// because it is the only realistic remote support mechanism for a team of volunteers, and
// that is precisely why NOTHING private may be inside it. §11.5 already forbids exporting
// the admin password and the WebDAV password; an archive is a wider door than an export,
// so this file closes it in two independent ways.
//
//  1. The configuration is REDACTED BY KEY NAME, recursively, over the whole JSON tree.
//     A driver option added in two years and called `token` is caught without anybody
//     remembering to come back here.
//  2. Every TEXT member of the archive is then SCRUBBED BY VALUE. This is the half that
//     is easy to forget and the half that actually leaks: a WebDAV source that cannot be
//     reached produces a technical journal line carrying the URL verbatim, from
//     net/http, and that line travels in health.json and in technical.csv. Redacting the
//     configuration alone would ship the secret anyway, through the journal.
//
// A test of this package looks for the values themselves INSIDE the produced archive.
// Reading the code is not evidence.

// Marker is what replaces a redacted value.
//
// It is a visible, French, greppable string and not an empty field: a reader must be able
// to tell « this station has no WebDAV password » from « the password was removed before
// the archive left ».
const Marker = "[caviardé]"

// sensitiveKeys are the configuration keys whose VALUE never leaves the station.
//
// Matched case-insensitively on the LAST segment of a dotted path, so that
// catalog.options.password and any nested group are covered by one entry.
var sensitiveKeys = map[string]bool{
	"password":           true,
	"password_hash":      true,
	"recovery_code_hash": true,
	"passphrase":         true,
	"secret":             true,
	"token":              true,
	"api_key":            true,
	"apikey":             true,
	"credential":         true,
	"credentials":        true,
	"private_key":        true,
}

// isSensitiveKey reports whether a value under this key must be redacted.
//
// Anything named url, or ending in _url, counts: §15.4 asks for an archive a volunteer can
// send to anybody, and the address of a cooperative's private Odoo host is not ours to
// publish. The scheme is preserved separately, because « https » or « http » is the one
// fact about that address a support call really needs.
func isSensitiveKey(key string) bool {
	lowered := strings.ToLower(key)
	if lowered == "url" || strings.HasSuffix(lowered, "_url") {
		return true
	}
	return sensitiveKeys[lowered]
}

// Redact renders a configuration fit to leave the station, as indented JSON.
//
// It goes through a generic JSON tree rather than through domain.Config for one reason: a
// key this build does not know about must still be CARRIED (the archive may be read by a
// newer support tool) and must still be REDACTED if its name says so. A struct-shaped
// redaction would silently drop the first and silently ship the second.
// It starts from the configuration ITSELF and not from Config.Export, and that is a
// deliberate difference from §11.5. Export DELETES the two secrets it knows about, which is
// right for a file meant to be re-imported and wrong for a diagnosis: a deleted key makes
// « ce poste n'a pas de mot de passe d'administration » — a real finding, since the « premier
// accès » journey is supposed to impose one — indistinguishable from « le mot de passe a été
// retiré avant l'envoi ». Redaction by key name is strictly stricter than Export anyway: it
// catches the two secrets Export knows and every one it does not.
func Redact(cfg domain.Config) ([]byte, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("configuration illisible avant caviardage : %w", err)
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, fmt.Errorf("configuration illisible avant caviardage : %w", err)
	}
	return json.MarshalIndent(redactTree(tree), "", "  ")
}

// redactTree walks a decoded JSON document and replaces what must not leave.
func redactTree(node any) any {
	switch value := node.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, child := range value {
			if isSensitiveKey(key) {
				out[key] = redactValue(key, child)
				continue
			}
			out[key] = redactTree(child)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, child := range value {
			out[i] = redactTree(child)
		}
		return out
	}
	return node
}

// redactValue replaces one sensitive value, keeping the ONE fact that is not private.
//
// For a URL that fact is the scheme, and it decides a remedy: an http source on a network
// somebody thought was TLS-protected is a finding, and it cannot be seen in an archive that
// replaced the whole field.
func redactValue(key string, value any) any {
	text, isText := value.(string)
	switch {
	case !isText:
		return Marker
	case text == "":
		// An empty secret is the ABSENCE of a secret, and it is information: a station with
		// no admin password runs the « premier accès » journey.
		return ""
	case isSensitiveURLKey(key):
		return redactURL(text)
	}
	return Marker
}

// isSensitiveURLKey reports whether this key carries an address rather than a secret.
func isSensitiveURLKey(key string) bool {
	lowered := strings.ToLower(key)
	return lowered == "url" || strings.HasSuffix(lowered, "_url")
}

// redactURL keeps the scheme and removes everything that identifies the host.
//
// A URL that does not parse is replaced WHOLE: a malformed private address is still a
// private address, and this is the one place where being conservative costs nothing.
func redactURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		return Marker
	}
	return parsed.Scheme + "://" + Marker
}

// --- The second door: scrubbing the archive BY VALUE ------------------------

// scrubber removes literal secret values from the text members of the archive.
//
// It exists because the configuration is not the only thing that carries them. A WebDAV
// source that refuses a connection produces `Get "https://user:pass@host/dav": dial tcp …`
// from net/http, that string lands in the technical journal, and the journal travels in
// diagnostic.zip. Redacting the configuration and shipping the journal would ship the
// secret.
type scrubber struct {
	// replacements maps a literal secret to what takes its place, LONGEST FIRST. The order
	// matters: replacing a host before the full URL that contains it would leave the
	// credentials of the URL behind.
	replacements []replacement
}

// replacement is one literal to remove, and what takes its place.
type replacement struct {
	from string
	to   string
}

// minScrubLength is the shortest literal this scrubber will chase.
//
// Four characters. A shorter secret would match ordinary words all over the archive — a
// password of « ab » would turn every « abandon » into « [caviardé]andon » and destroy the
// document it is protecting. A station whose WebDAV password is three characters long has a
// problem this file cannot fix, and the configuration member redacts it by key name anyway.
const minScrubLength = 4

// newScrubber collects the literal values this station must not publish.
func newScrubber(cfg domain.Config) *scrubber {
	s := &scrubber{}
	s.add(cfg.Admin.PasswordHash, Marker)
	s.add(cfg.Admin.RecoveryCodeHash, Marker)
	s.collect(cfg.Catalog.Options)
	s.collect(cfg.Printer.Options)
	s.collect(cfg.Scale.Options)

	// Longest first, so that a URL is removed before the host it contains.
	sort.SliceStable(s.replacements, func(i, j int) bool {
		return len(s.replacements[i].from) > len(s.replacements[j].from)
	})
	return s
}

// collect walks one driver option block and adds every sensitive value it carries.
func (s *scrubber) collect(options domain.DriverOptions) {
	for _, key := range options.Keys() {
		if group, ok := options.Group(key); ok {
			s.collect(group)
			continue
		}
		value, ok := options.Text(key)
		if !ok || !isSensitiveKey(key) {
			continue
		}
		if isSensitiveURLKey(key) {
			s.addURL(value)
			continue
		}
		s.add(value, Marker)
	}
}

// addURL adds a URL, its host and its embedded credentials as separate literals.
//
// Three entries and not one, because an error message rarely carries the whole URL: net/http
// quotes the full address, a DNS failure quotes the HOST alone, and a proxy quotes the
// userinfo. Removing only the form that appears in the configuration would leave the other
// two in the journal.
func (s *scrubber) addURL(raw string) {
	s.add(raw, redactURL(raw))
	parsed, err := url.Parse(raw)
	if err != nil {
		return
	}
	s.add(parsed.Host, Marker)
	s.add(parsed.Hostname(), Marker)
	if parsed.User != nil {
		s.add(parsed.User.String(), Marker)
		if password, set := parsed.User.Password(); set {
			s.add(password, Marker)
		}
	}
}

// add records one literal, unless it is too short to chase safely.
func (s *scrubber) add(from, to string) {
	if len(from) < minScrubLength {
		return
	}
	for _, existing := range s.replacements {
		if existing.from == from {
			return
		}
	}
	s.replacements = append(s.replacements, replacement{from: from, to: to})
}

// Clean removes every collected literal from one text member.
func (s *scrubber) Clean(text string) string {
	for _, r := range s.replacements {
		text = strings.ReplaceAll(text, r.from, r.to)
	}
	return text
}

// CleanBytes is Clean over a byte slice, which is the shape most members have.
func (s *scrubber) CleanBytes(raw []byte) []byte {
	if len(s.replacements) == 0 {
		return raw
	}
	return []byte(s.Clean(string(raw)))
}

// faultLine renders one configuration fault for a report that travels.
//
// The message of a fault about a sensitive field ECHOES THE VALUE — domain's OptionURL
// control answers « "https://user:pass@host/dav" n'est pas une URL http ou https absolue »,
// quoting it — so the message is replaced rather than quoted. The FIELD is kept, because
// the field is what a volunteer has to go and fix.
func faultLine(fault domain.Fault) string {
	last := fault.Field
	if i := strings.LastIndex(last, "."); i >= 0 {
		last = last[i+1:]
	}
	if isSensitiveKey(last) {
		return fault.Field + " : valeur refusée, non reproduite ici (contenu sensible)"
	}
	return fault.String()
}
