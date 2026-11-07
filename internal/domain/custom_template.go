package domain

import "strings"

// This file holds what makes a cooperative's OWN label layout coexist with the ones
// compiled into the binary: the single Go-side starting point the admin screen's
// "Partir du gabarit livré" button copies from (decision 12), the one place that
// resolves "the gabarit in service" for a station that may have declared one
// (decision 11), and control 53, which refuses what would make that coexistence
// silent -- a nameless layout, one that recovers a shipped name, an offset carried by
// the wrong field (ADR-062).
//
// The nine hard rules of §7.5 (template_validate.go) judge GEOMETRY; this file never
// repeats them. What it adds is IDENTITY: a layout that passes every one of them can
// still be refused here for naming itself "weighing_identical".

// StarterTemplateName is the name StarterTemplate ships under. It never collides
// with a shipped layout (control 53 only refuses printer.custom_template.name
// weighing_identical or weighing_neutral_single), so a fresh draft produced by the
// button validates before the volunteer has changed a single field.
const StarterTemplateName = "cooperative"

// StarterTemplate is the single source the admin screen's "Partir du gabarit livré"
// button (decision 12) copies from: weighing_identical, renamed, with its offsets
// zeroed because printer.options.offset_x/_y carry the station's shift and never the
// gabarit itself (control 53).
//
// A SINGLE source, deliberately: a second copy of this geometry kept in TypeScript
// would drift from this one the day either changes, and nothing would say so. The
// button therefore asks Go for this value rather than re-encoding weighing_identical
// client-side.
func StarterTemplate() Template {
	starter := IdenticalTemplate()
	starter.Name = StarterTemplateName
	starter.OffsetXDots, starter.OffsetYDots = 0, 0
	return starter
}

// Templates reports every label layout this station may choose from: what the
// registry carries (reg.templates(), today ShippedTemplates()), plus the
// cooperative's own layout under its own name -- UNLESS that name already belongs to
// a shipped one, in which case the shipped layout wins and the cooperative's is
// simply absent from the map (decision 9). Overriding it would change what a
// configuration that already named that layout prints, silently, the moment a
// cooperative declares one under the same name -- exactly the kind of departure A1
// exists to rule out for weighing_identical.
//
// The operator's offset is RECOMPOSED into every layout returned, shipped or not: it
// is a setting of the STATION (printer.options.offset_x/_y), not of any one gabarit,
// and the three controls that read a label's geometry (21, 29, 38) must see it
// applied the same way whichever name printer.template holds.
func (c *Config) Templates(reg Registries) map[string]Template {
	offsetX, _ := intOption(c.Printer.Options, "offset_x")
	offsetY, _ := intOption(c.Printer.Options, "offset_y")

	shipped := reg.templates()
	out := make(map[string]Template, len(shipped)+1)
	for name, template := range shipped {
		template.OffsetXDots, template.OffsetYDots = offsetX, offsetY
		out[name] = template
	}
	if custom := c.Printer.CustomTemplate; custom != nil {
		if _, shadowsShipped := shipped[custom.Name]; !shadowsShipped {
			template := custom.clone()
			template.OffsetXDots, template.OffsetYDots = offsetX, offsetY
			out[template.Name] = template
		}
	}
	return out
}

// TemplateNames reports the names Templates(reg) would accept, sorted: what the
// admin screen's selector lists and what control 29 offers in Fault.Values when
// printer.template names none of them.
func (c *Config) TemplateNames(reg Registries) []string {
	return sortedKeys(c.Templates(reg))
}

// SelectedTemplate resolves printer.template the way reg.Template did before this
// lot, except that it now also sees the cooperative's own layout: labelGeometry, and
// through it controls 21, 29 and 38, must all see the SAME gabarit, whichever one a
// station actually prints with.
func (c *Config) SelectedTemplate(reg Registries) (Template, bool) {
	template, ok := c.Templates(reg)[c.Printer.Template]
	return template, ok
}

// validateCustomTemplate is control 53: the cooperative's own layout, if any, must be
// able to coexist with the registry, something the nine hard rules never judge
// because they know nothing of names or of where an offset belongs.
//
// It runs whenever printer.custom_template is present, whether or not
// printer.template currently names it: the "Partir du gabarit livré" button
// (decision 12) writes it into the draft before the volunteer has necessarily
// selected it, and a blank name or a name that already belongs to a shipped layout
// is wrong on sight, not only once chosen.
func (c *Config) validateCustomTemplate(reg Registries) []Fault {
	var faults faultList

	custom := c.Printer.CustomTemplate
	if custom == nil {
		return faults
	}

	if strings.TrimSpace(custom.Name) == "" {
		faults.add("printer.custom_template.name",
			"le gabarit de la coopérative doit porter un nom : c'est lui que printer.template désigne")
		return faults
	}

	if _, shipped := reg.templates()[custom.Name]; shipped {
		faults.addChoice("printer.custom_template.name", reg.TemplateNames(),
			"%q est le nom d'un gabarit livré : le gabarit de la coopérative en porte un autre, "+
				"pour ne jamais changer ce qu'imprime un poste qui le nommait",
			custom.Name)
	}

	// The ±1 dot arrows of the admin screen write to printer.options.offset_x/_y, and
	// Templates(reg) above recomposes that single value into every gabarit it
	// returns, custom one included. A second value carried BY the gabarit would
	// either be redundant with the station's or, the day it is not, print a shift
	// nothing on the admin screen shows.
	if custom.OffsetXDots != 0 {
		faults.add("printer.custom_template.offset_x_dots",
			"le décalage se règle dans printer.options.offset_x, pas dans le gabarit")
	}
	if custom.OffsetYDots != 0 {
		faults.add("printer.custom_template.offset_y_dots",
			"le décalage se règle dans printer.options.offset_y, pas dans le gabarit")
	}

	return faults
}

// clone returns a deep copy of t: Elements is a slice, and handing out the SAME one
// would let a caller editing a copy -- the export of §11.5, a config read back after
// Templates(reg) -- reach into the configuration the station is running on.
func (t Template) clone() Template {
	out := t
	out.Elements = append([]Element(nil), t.Elements...)
	return out
}
