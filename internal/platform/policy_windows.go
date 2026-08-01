package platform

import (
	"fmt"
	"sort"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// writeUserPolicies writes the values under HKEY_CURRENT_USER, list keys first emptied.
//
// # The lists are DELETED before being rewritten
//
// Chromium reads URLAllowlist as « every value of this subkey », so a station whose
// address changed from :8085 to :8086 would keep BOTH — the old entry silently reopening
// the door this whole mechanism closes. Nothing else on a policy key behaves like that,
// which is why only the subkeys these values name are cleared, and never the root: a root
// wiped at every logon would take with it anything an administrator posed by hand.
func writeUserPolicies(root string, values []PolicyValue) (int, error) {
	if err := clearLists(root, values); err != nil {
		return 0, err
	}
	written := 0
	for _, value := range values {
		if err := writeOne(root, value); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

// clearLists removes every subkey the values are about to fill.
//
// A key that is not there is not an error: the first logon of a station has none of them.
func clearLists(root string, values []PolicyValue) error {
	for _, list := range listKeys(values) {
		err := registry.DeleteKey(registry.CURRENT_USER, root+`\`+list)
		if err != nil && err != registry.ErrNotExist {
			return fmt.Errorf("la liste de stratégie %s n'a pas pu être vidée : %w", list, err)
		}
	}
	return nil
}

// listKeys is the sorted set of subkeys the values name.
//
// Sorted so that a failure names the same key twice in a row: an error message that
// depends on the iteration order of a map is an error message two volunteers report
// differently.
func listKeys(values []PolicyValue) []string {
	seen := make(map[string]bool)
	for _, value := range values {
		if value.Key != "" {
			seen[value.Key] = true
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// writeOne creates the key if needed and writes the value in its declared type.
func writeOne(root string, value PolicyValue) error {
	path := root
	if value.Key != "" {
		path += `\` + value.Key
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("la clé de stratégie %s n'a pas pu être ouverte : %w", path, err)
	}
	defer func() { _ = key.Close() }()

	switch value.Kind {
	case PolicyDWord:
		err = key.SetDWordValue(value.Name, value.Number)
	default:
		err = key.SetStringValue(value.Name, value.Text)
	}
	if err != nil {
		return fmt.Errorf("la stratégie %s n'a pas pu être écrite : %w",
			strings.TrimPrefix(path+`\`+value.Name, `\`), err)
	}
	return nil
}
