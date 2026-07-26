package importer

import (
	"context"
	"fmt"
	"strings"

	"openscale/internal/catalog"
	"openscale/internal/domain"
)

// namedMotives is how many majority motives the refusal names.
//
// Three, as §10.4b writes it, and the number matters: one motive reads like a guess,
// the whole list reads like a dump, and three with an example line each is a work plan
// somebody can take to Odoo (§10.3 bis).
const namedMotives = 3

// amputated applies the RELATIVE guard of §10.4b and reports why, in French.
//
// It bears on the number of PESABLES from one applied import to the next, and that is
// the correction the whole guard exists for: a column shift at the producer makes the
// weighable products collapse without touching the number of readable lines, so the
// absolute guard of §10.4a — which counts lines that are not products at all — sees
// nothing at all.
//
// A station that has never applied a catalog has nothing to compare against, and its
// first import is therefore never refused by this guard: the alternative would be a
// station that cannot be filled.
func (a *Applier) amputated(ctx context.Context, cfg domain.Config, report catalog.Report) (string, bool) {
	previous, err := a.records.LastAppliedImport(ctx)
	if err != nil || previous.Weighable <= 0 {
		return "", false
	}
	kept := keptPerMille(cfg)
	if report.Weighable*1000 >= kept*previous.Weighable {
		return "", false
	}
	return amputatedReason(report, previous.Weighable, kept), true
}

// keptPerMille is the share of the previous count an import must still carry, in per
// mille.
//
// Per mille and on integers, exactly like the absolute guard: max_weighable_drop is one
// of the two ratios a configuration carries, and a threshold has no business being
// decided by a rounding error.
func keptPerMille(cfg domain.Config) int {
	drop := defaultMaxWeighableDrop
	if value, ok := cfg.Catalog.Options.Ratio("max_weighable_drop"); ok && value >= 0 && value <= 1 {
		drop = value
	}
	return 1000 - int(drop*1000+0.5)
}

// amputatedReason is the sentence a volunteer reads, and the one the .reason.txt
// carries next to the archived copy.
//
// It names the two counts, the threshold, and the three majority motives WITH an
// example line: « le lot n'est pas appliqué » is a wall, « 214 produits préemballés de
// plus qu'hier, par exemple ligne 87 » is a diagnosis (§10.4b, §10.3 bis).
func amputatedReason(report catalog.Report, previous, kept int) string {
	return fmt.Sprintf(
		"%d produit%s pesable%s reçu%s contre %d au dernier import appliqué, en dessous du "+
			"seuil de %d %% ; motifs majoritaires : %s. Le catalogue précédent reste en "+
			"service : un catalogue amputé est presque toujours un export Odoo qui a mal "+
			"tourné, pas une décision.",
		report.Weighable, plural(report.Weighable), plural(report.Weighable), plural(report.Weighable),
		previous, kept/10, listMotives(report.Motives()))
}

// listMotives names the majority motives, most frequent first, with an example row.
func listMotives(motives []catalog.Motive) string {
	if len(motives) == 0 {
		return "aucun signalement, ce qui désigne le fichier lui-même et non ses lignes"
	}
	if len(motives) > namedMotives {
		motives = motives[:namedMotives]
	}
	named := make([]string, 0, len(motives))
	for _, m := range motives {
		named = append(named, fmt.Sprintf("%s (%d ligne%s, par exemple ligne %d)",
			m.Code, m.Count, plural(m.Count), m.CSVLine))
	}
	return strings.Join(named, ", ")
}

// bannedReason is what a content that has already failed enough times is told.
//
// It names the remedy, and it has to: a producer who corrects the file and drops it
// again with byte-identical content would otherwise stay banned for ever, with nothing
// on the screen saying which button undoes it (§10.5).
func bannedReason(entry domain.QuarantineEntry) string {
	return fmt.Sprintf(
		"ce contenu a déjà été refusé %d fois, la première le %s ; motif : %s. "+
			"Il n'est plus relu. Corriger le fichier chez le producteur, ou utiliser "+
			"« Oublier la quarantaine » dans l'onglet Catalogue.",
		entry.FailureCount, entry.FirstFailureAt.Format("02/01/2006 à 15:04"), entry.Reason)
}

// plural is the French mark of the plural, which starts at two.
func plural(n int) string {
	if n > 1 {
		return "s"
	}
	return ""
}
