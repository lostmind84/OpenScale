// This file holds what a DRIVER OPTION reads back as -- without a float ever
// carrying a quantity -- and what a registry answers when it carries no template of
// its own.

package domain

import "testing"

func TestDriverOptionsReadTheirValuesWithoutAFloat(t *testing.T) {
	options := DriverOptions{}
	setOption(t, options, "port", "COM8")
	setOption(t, options, "baud", 9600)
	setOption(t, options, "invert_bits", false)
	setOption(t, options, "min_readable_ratio", 0.9)
	setOption(t, options, "fallback", map[string]any{"enabled": true})

	if value, ok := options.Text("port"); !ok || value != "COM8" {
		t.Errorf("port = %q, %v", value, ok)
	}
	if value, ok := options.Int("baud"); !ok || value != 9600 {
		t.Errorf("baud = %d, %v", value, ok)
	}
	// A baud rate is not a ratio: reading a whole number as one must not silently
	// succeed through a float.
	if _, ok := options.Int("min_readable_ratio"); ok {
		t.Error("0,9 n'est pas un entier")
	}
	if value, ok := options.Ratio("min_readable_ratio"); !ok || value != 0.9 {
		t.Errorf("min_readable_ratio = %v, %v", value, ok)
	}
	if value, ok := options.Bool("invert_bits"); !ok || value {
		t.Errorf("invert_bits = %v, %v", value, ok)
	}
	if group, ok := options.Group("fallback"); !ok {
		t.Error("fallback doit se lire comme un objet")
	} else if enabled, ok := group.Bool("enabled"); !ok || !enabled {
		t.Error("fallback.enabled doit se lire depuis le groupe")
	}
	if _, ok := options.Text("absent"); ok {
		t.Error("une option absente ne doit pas se lire")
	}
	if got := options.Keys(); len(got) != 5 || got[0] != "baud" {
		t.Errorf("Keys() = %v, il doit être trié", got)
	}
}

func TestRegistriesFallBackOnTheCompiledTemplates(t *testing.T) {
	var empty Registries
	if _, ok := empty.Template(DefaultTemplateName); !ok {
		t.Fatalf("un registre vide doit servir les gabarits compilés, %q absent", DefaultTemplateName)
	}
	if got := empty.TemplateNames(); len(got) != len(ShippedTemplates()) {
		t.Fatalf("gabarits = %v, attendu les %d gabarits livrés", got, len(ShippedTemplates()))
	}
	if _, ok := empty.Template("weighing_imaginaire"); ok {
		t.Error("un gabarit inexistant ne doit pas se résoudre")
	}
}
