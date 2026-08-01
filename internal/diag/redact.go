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

// isSensitiveKey reports whether a value under this key must be redacted.
//
// The names of the SECRETS come from domain.IsSecretOptionKey, and they are shared rather
// than copied on purpose: Config.Export refuses to carry exactly this list, and an archive
// must never be less strict than an export. Adding a name in one place must protect both
// doors, or the next driver option called `token` leaves through whichever door was
// forgotten. Matched case-insensitively on the LAST segment of a dotted path, so that
// catalog.options.password and any nested group are covered by one entry.
//
// What this side adds is the ADDRESS: anything named url, or ending in _url, counts here
// and not in the export. §15.4 asks for an archive a volunteer can send to anybody, and
// the address of a cooperative's private Odoo host is not ours to publish; an export is
// re-imported by the fleet that owns that host, so domain treats a URL as a SITE value and
// keeps it in a hardware export. The scheme is preserved separately, because « https » or
// « http » is the one fact about that address a support call really needs.
func isSensitiveKey(key string) bool {
	lowered := strings.ToLower(key)
	if lowered == "url" || strings.HasSuffix(lowered, "_url") {
		return true
	}
	return domain.IsSecretOptionKey(lowered)
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
//
// The DECODING faults are taken as an argument rather than worked out here, because they
// were worked out once already, by the only function that can: a block replaced by the
// neutral profile is indistinguishable, after the fact, from a block a station really
// declared that way. What they earn is a sentence in _readme, and the reasons that is the
// right place are in warnSubstitutedBlocks.
func Redact(cfg domain.Config, faults []domain.Fault) ([]byte, error) {
	cfg.Readme = warnSubstitutedBlocks(cfg.Readme, faults)
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

// warnSubstitutedBlocks puts, at the HEAD of _readme, the fact that part of this document
// is not what the station declared.
//
// # Why _readme and nowhere else
//
// A block that will not decode falls back on the neutral profile, so this member carried
// the FACTORY grid presented as the shop's own -- read six months later, remotely, by
// somebody with no way of telling. Three other places were considered and each broke
// something this one does not: dropping the member takes the whole configuration away from
// the station that has 13 readable blocks out of 14, which is exactly the station support
// needs to see; shipping the RAW bytes puts admin.password_hash, the recovery hash and the
// WebDAV credentials back in a file whose whole promise is « vous pouvez l'envoyer sans le
// relire »; and a header outside the JSON breaks every machine that reads it.
//
// Config.Readme is « the mode d'emploi that JSON cannot carry as a comment » (config.go),
// it keeps the document valid JSON, it goes through the same redaction as everything else,
// and it is OUT OF THE FINGERPRINT by construction -- so nothing that gets compared moves
// because of it.
//
// It PREPENDS: whatever the file already explained is still true and still worth reading,
// just not first.
//
// A document that did not decode AT ALL never reaches here -- Doctor.readConfiguration
// returns it unparsed and the archive writes a failure member instead -- so every fault
// this sees names a block.
func warnSubstitutedBlocks(readme string, faults []domain.Fault) string {
	if len(faults) == 0 {
		return readme
	}
	blocks := make([]string, 0, len(faults))
	for _, fault := range faults {
		blocks = append(blocks, "« "+fault.Field+" »")
	}

	warning := fmt.Sprintf("ATTENTION : le bloc %s de ce poste n'a pas pu être lu, et ce "+
		"qui figure ci-dessous à sa place est la configuration D'USINE, pas celle du poste. "+
		"Ne recopiez pas ces valeurs-là et ne les prenez pas pour un réglage : elles ne "+
		"viennent de personne. Le reste du document est bien celui du fichier.",
		strings.Join(blocks, ", "))
	if len(blocks) > 1 {
		warning = fmt.Sprintf("ATTENTION : les blocs %s de ce poste n'ont pas pu être lus, "+
			"et ce qui figure ci-dessous à leur place est la configuration D'USINE, pas celle "+
			"du poste. Ne recopiez pas ces valeurs-là et ne les prenez pas pour un réglage : "+
			"elles ne viennent de personne. Le reste du document est bien celui du fichier.",
			strings.Join(blocks, ", "))
	}

	if readme == "" {
		return warning
	}
	return warning + " — " + readme
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
