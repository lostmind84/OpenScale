package domain

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// This file holds controls 6, 7 and 9 -- the ones that judge an option map against
// the schema THE CHOSEN DRIVER declares, rather than against anything this package
// knows.
//
// It is separate from validate.go for that reason: the 48 controls know what a
// configuration means, these know only what a driver said about itself.

// validateOptions reports every fault the options of one driver break against the
// schema THE DRIVER DECLARES.
//
// An unregistered driver -- no descriptor at all -- yields no fault: inventing a
// schema for a driver that has not been written yet would be a second source of
// truth for something the driver owns (ADR-025).
//
// family is the whole list the descriptor was drawn from — every scale, every printer,
// every catalog source this binary carries. It is read for ONE purpose: telling somebody
// which driver declares the key they typed under the wrong one.
func validateOptions(field string, options DriverOptions, descriptor *DriverDescriptor,
	family []DriverDescriptor) []Fault {
	if descriptor == nil {
		return nil
	}
	var faults []Fault
	declared := make(map[string]bool, len(descriptor.Options))
	names := make([]string, 0, len(descriptor.Options))
	for _, schema := range descriptor.Options {
		declared[schema.Key] = true
		names = append(names, schema.Key)
	}
	sort.Strings(names)

	for _, schema := range descriptor.Options {
		path := field + "." + schema.Key
		raw, ok := options[schema.Key]
		if !ok || (schema.Required && isEmptyText(raw)) {
			if schema.Required {
				faults = append(faults, Fault{
					Field:   path,
					Message: fmt.Sprintf("option exigée par le driver %q", descriptor.ID),
				})
			}
			continue
		}
		faults = append(faults, schema.check(path, raw)...)
	}
	for _, key := range options.Keys() {
		if declared[key] {
			continue
		}
		// A key nobody declared is a refusal; a key ANOTHER driver of the same family
		// declares is a piece of advice, and it is the one that matters — `directory`
		// under a WebDAV share, `username` under a local drop, `queue` under a TCP
		// transport are all the same mistake: the right key, the wrong driver. Saying so
		// is what the two dedicated controls that used to name `local_drop` and `webdav`
		// by hand were really worth (ADR-052).
		message := fmt.Sprintf("option inconnue du driver %q", descriptor.ID)
		if declaredBy := driversDeclaring(family, key, descriptor.ID); len(declaredBy) > 0 {
			message = fmt.Sprintf("%s : c'est %s qui la déclare", message,
				quotedList(declaredBy))
		}
		faults = append(faults, Fault{Field: field + "." + key, Message: message, Values: names})
	}
	return faults
}

// quotedList spells a list of driver names the way a fault reads it aloud.
func quotedList(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, fmt.Sprintf("%q", name))
	}
	if len(quoted) < 2 {
		return strings.Join(quoted, "")
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " ou " + quoted[len(quoted)-1]
}

// isEmptyText reports whether a raw option value is the empty string, which is how a
// file spells a field nobody filled in.
//
// It is what makes a REQUIRED option refuse `"port": ""` the way it refuses a missing
// key: the two are the same thing for whoever is standing in front of the station, and
// the schema check alone would accept the empty string as a perfectly good text value.
// An optional option, on the contrary, is legitimately empty — `address` is empty on
// every station whose transport is winspool.
func isEmptyText(raw json.RawMessage) bool {
	value, ok := DriverOptions{"": raw}.Text("")
	return ok && value == ""
}

// check reports the faults one raw value breaks against this schema entry.
func (s OptionSchema) check(field string, raw json.RawMessage) []Fault {
	fault := func(format string, args ...any) []Fault {
		return []Fault{{Field: field, Message: fmt.Sprintf(format, args...)}}
	}
	single := DriverOptions{s.Key: raw}
	switch s.Kind {
	case OptionText:
		if _, ok := single.Text(s.Key); !ok {
			return fault("attendu : %s", s.Kind)
		}
	case OptionBool:
		if _, ok := single.Bool(s.Key); !ok {
			return fault("attendu : %s", s.Kind)
		}
	case OptionInt:
		value, ok := single.Int(s.Key)
		if !ok {
			return fault("attendu : %s", s.Kind)
		}
		if s.Max != 0 && (value < s.Min || value > s.Max) {
			return fault("%d hors bornes [%d, %d]", value, s.Min, s.Max)
		}
	case OptionRatio:
		value, ok := single.Ratio(s.Key)
		if !ok {
			return fault("attendu : %s", s.Kind)
		}
		// The bounds are declared IN PER MILLE, so no float ever enters a
		// declaration; the comparison converts once, here.
		if s.Max != 0 && (value < float64(s.Min)/1000 || value > float64(s.Max)/1000) {
			return fault("%v hors bornes [%v, %v]", value, float64(s.Min)/1000, float64(s.Max)/1000)
		}
	case OptionEnum:
		value, ok := single.Text(s.Key)
		if !ok {
			return fault("attendu : %s", s.Kind)
		}
		if len(s.Values) > 0 && !known(s.Values, value) {
			return []Fault{{
				Field:   field,
				Message: fmt.Sprintf("valeur inconnue %q", value),
				Values:  s.Values,
			}}
		}
	case OptionHostPort:
		value, ok := single.Text(s.Key)
		if !ok {
			return fault("attendu : %s", s.Kind)
		}
		if value == "" {
			return nil // an unused option, such as address when the transport is winspool
		}
		if err := checkHostPort(value); err != nil {
			return fault("%q n'est pas une adresse hôte:port valide (%s)", value, err)
		}
	case OptionURL:
		value, ok := single.Text(s.Key)
		if !ok {
			return fault("attendu : %s", s.Kind)
		}
		if value != "" && !isHTTPURL(value) {
			return fault("%q n'est pas une URL http ou https absolue", value)
		}
	case OptionGroup:
		nested, ok := single.Group(s.Key)
		if !ok {
			return fault("attendu : %s", s.Kind)
		}
		// A nested group has no family: the only driver that could declare its keys is the
		// one that declared the group, so there is nobody to point at.
		return validateOptions(field, nested, &DriverDescriptor{ID: s.Key, Options: s.Options}, nil)
	}
	return nil
}
