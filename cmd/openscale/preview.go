package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"strings"

	"openscale/internal/domain"
	"openscale/internal/printing"
	"openscale/internal/web"
)

// This file renders the aperçu of the label THROUGH THE SAME RENDERER THAT PRINTS
// (decision A2): an aperçu produced by a second code path would be a picture of what
// somebody hoped the printer would do.

// LabelPreview renders the label as a PNG, through the SAME renderer that prints (A2).
//
// One renderer and not two is the whole of decision A2: an aperçu produced by a second
// code path would be a picture of what somebody hoped the printer would do. The offset is
// recomposed into the template on every call, so that a volunteer pressing the ±1 dot
// arrow sees the label move.
func (h adminHardware) LabelPreview(_ context.Context, q web.PreviewQuery) ([]byte, error) {
	cfg := h.hub.Config()
	templates, err := templatesFor(cfg, h.registries)
	if err != nil {
		return nil, err
	}
	name := q.Template
	if name == "" {
		name = cfg.Printer.Template
	}
	template, known := templates[name]
	if !known {
		return nil, fmt.Errorf("gabarit %q inconnu ; gabarits disponibles : %s",
			name, strings.Join(h.registries.TemplateNames(), ", "))
	}

	image, err := h.previewImage(cfg, template, q)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := printing.EncodePNG(&out, image); err != nil {
		return nil, fmt.Errorf("aperçu non encodé : %w", err)
	}
	return out.Bytes(), nil
}

// previewImage draws either the demonstration label or the one the station is holding.
//
// The station's own label is the default because that is what makes the aperçu a
// verification rather than an illustration: after a weighing, the screen shows the very
// label that came out. Demo is what the settings screen asks for while nobody is weighing.
func (h adminHardware) previewImage(cfg domain.Config, template domain.Template,
	q web.PreviewQuery) (*image.Gray, error) {
	if q.Demo {
		rules := cfg.Pricing
		if q.Dual {
			// The two-tier grid of the document, so that an operator sees the crowded case
			// — the one where a field can overflow — without having to configure it first.
			rules = domain.LaCagetteRules()
		}
		image, _, err := renderDemo(template, rules, printing.RenderOptions{})
		return image, err
	}

	snapshot := h.hub.State()
	label := snapshot.Label
	if label == nil {
		label = snapshot.LastLabel
	}
	if label == nil {
		return nil, errors.New("aucune étiquette en cours sur ce poste : demandez l'aperçu de " +
			"démonstration (demo=1), ou pesez un produit")
	}
	return printing.Rasterize(&template, *label, domain.LocaleFrench, printing.RenderOptions{})
}
