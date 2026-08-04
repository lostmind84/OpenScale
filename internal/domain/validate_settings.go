package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// This file holds the controls that judge WHAT A COOPERATIVE SETTLED: the price
// grid, the weighing bounds, the stability policy, what the station keeps, who may
// write to it, where the products come from and where it looks for a newer version
// of itself.
//
// They are the other half of the list validate.go opens, and they carry the numbers
// §11.3 gave them: 10 to 16, 22 to 28, 30 to 36, 39 and 40, 43 to 46, 48 and 49.
// Validate calls both halves in numeric order, and never in the order either file
// writes them.
//
// Every one of them bears on a decision somebody TOOK, which is why their messages
// name a shop and not a machine -- « la grille de tarifs est vide », « une lettre
// hors F/L/V/A n'aurait plus où atterrir ».

// repositoryShape is control 48: an owner and a repository, nothing else. No
// scheme, no host, no dots that climb, no third segment.
var repositoryShape = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,39}/[A-Za-z0-9_.-]{1,100}$`)

// validatePricing is controls 10 to 16: the grid holds at least one tier, its codes
// are unique and bounded, and the three roles name tiers that exist.
func (c *Config) validatePricing() []Fault {
	var faults faultList

	// 10. At least one tier. Dual pricing is not a boolean, it is the cardinality of
	//     the grid (§6.3).
	if len(c.Pricing.Tiers) == 0 {
		faults.add("pricing.tiers", "la grille de tarifs est vide : il en faut au moins un")
	}
	codes := make(map[string]bool, len(c.Pricing.Tiers))
	for i, tier := range c.Pricing.Tiers {
		// 11. The tier reference_code names is the catalog price -- the one the
		//     till charges. Its discount is not a setting, it is zero by
		//     definition, so a file that gives it one is REFUSED rather than
		//     quietly obeyed (ADR-034).
		if tier.Code == c.Pricing.ReferenceCode && tier.Discount != 0 {
			faults.add(fmt.Sprintf("pricing.tiers[%d].discount_percent", i),
				"le tarif de référence est le prix du catalogue : il ne porte pas de remise")
		}
		// 12. Codes unique: the code is the key of a tier, in the file, on the label
		//     and in the journal.
		if codes[tier.Code] {
			faults.add(fmt.Sprintf("pricing.tiers[%d].code", i), "le code %q est déclaré deux fois", tier.Code)
		}
		codes[tier.Code] = true
		// 13. A discount is a percentage between 0 and 100. A hundred is free, and
		//     that is a grid a cooperative may legitimately declare.
		if tier.Discount < 0 || tier.Discount > FullDiscount {
			faults.add(fmt.Sprintf("pricing.tiers[%d].discount_percent", i),
				"%s %% n'est pas une remise entre 0 et 100 %%", tier.Discount)
		}
	}
	tierCodes := make([]string, 0, len(c.Pricing.Tiers))
	for _, tier := range c.Pricing.Tiers {
		tierCodes = append(tierCodes, tier.Code)
	}

	// 14. primary_code belongs to the grid: it is the price printed LARGE (A7).
	if !codes[c.Pricing.PrimaryCode] {
		faults.addChoice("pricing.primary_code", tierCodes, "%q ne désigne aucun tarif de la grille", c.Pricing.PrimaryCode)
	}
	// 15. reference_code belongs to the grid: it is the one encoded when the payload
	//     carries a price, and the till must never under-charge.
	if !codes[c.Pricing.ReferenceCode] {
		faults.addChoice("pricing.reference_code", tierCodes, "%q ne désigne aucun tarif de la grille", c.Pricing.ReferenceCode)
	}
	// 16. Each secondary code belongs to the grid.
	for i, code := range c.Pricing.SecondaryCodes {
		if !codes[code] {
			faults.addChoice(fmt.Sprintf("pricing.secondary_codes[%d]", i), tierCodes,
				"%q ne désigne aucun tarif de la grille", code)
		}
	}
	return faults
}

// validateLimits is controls 22 to 25: the weighing windows, each bounded by what
// a field of the barcode can carry rather than by plausibility.
func (c *Config) validateLimits() []Fault {
	var faults faultList

	// 22. basket_min_g ≤ basket_max_g ≤ 0: the window means "the customer lifted off
	//     a basket the scale was tared for", so it is NEGATIVE by nature.
	if c.Limits.BasketMin > c.Limits.BasketMax || c.Limits.BasketMax > 0 {
		faults.add("limits.basket_min_g",
			"la fenêtre du panier (%d ≤ %d ≤ 0) est incohérente : elle décrit un poids négatif",
			c.Limits.BasketMin, c.Limits.BasketMax)
	}

	// 23. min_weight_g < max_weight_g ≤ 99999. The ceiling is the CAPACITY of the
	//     NNDDD field of the barcode, not a plausibility threshold.
	if c.Limits.MinWeight >= c.Limits.MaxWeight || c.Limits.MaxWeight > MaxWeight {
		faults.add("limits.max_weight_g",
			"les bornes de poids (%d < %d ≤ %d) sont incohérentes : %d g est la capacité du champ NNDDD du code-barres",
			c.Limits.MinWeight, c.Limits.MaxWeight, MaxWeight, MaxWeight)
	}

	// 24. min_units ≤ max_units ≤ 99: two digits in the payload of prefix 0499.
	if c.Limits.MinUnits > c.Limits.MaxUnits || c.Limits.MaxUnits > 99 {
		faults.add("limits.max_units",
			"les bornes d'unités (%d ≤ %d ≤ 99) sont incohérentes : la charge utile du préfixe à l'unité fait deux chiffres",
			c.Limits.MinUnits, c.Limits.MaxUnits)
	}

	// 25. max_amount_cents ≤ 99999.
	if c.Limits.MaxAmount > 99_999 {
		faults.add("limits.max_amount_cents", "%d dépasse la capacité du champ de prix du code-barres (99 999 centimes)",
			c.Limits.MaxAmount)
	}
	return faults
}

// validateStability is controls 26 to 28: a stability window that can actually be
// held, an expiry that outlives one measurement, and two words of a closed list.
func (c *Config) validateStability() []Fault {
	var faults faultList

	// 26. timeout_ms > min_duration_ms: a window that expires before it can hold
	//     would time out every single weighing.
	if c.Stability.Timeout <= c.Stability.MinDuration {
		faults.add("stability.timeout_ms", "%s doit dépasser la durée de stabilité exigée (%s)",
			c.Stability.Timeout, c.Stability.MinDuration)
	}

	// 27. expiry_floor_ms ≥ 1000 and < expiry_ceiling_ms.
	if c.Stability.ExpiryFloor < Duration(time.Second) {
		faults.add("stability.expiry_floor_ms", "%s est sous le plancher de 1 s : le poids serait déclaré périmé avant la mesure suivante",
			c.Stability.ExpiryFloor)
	}
	if c.Stability.ExpiryFloor >= c.Stability.ExpiryCeiling {
		faults.add("stability.expiry_ceiling_ms", "%s doit dépasser le plancher de péremption (%s)",
			c.Stability.ExpiryCeiling, c.Stability.ExpiryFloor)
	}

	// 28. stability.mode and on_timeout in the list (A3).
	if !known(stabilityModes(), c.Stability.Mode) {
		faults.addChoice("stability.mode", stabilityModes(), "mode inconnu %q", c.Stability.Mode)
	}
	if !known(timeoutActions(), c.Stability.OnTimeout) {
		faults.addChoice("stability.on_timeout", timeoutActions(), "action inconnue %q", c.Stability.OnTimeout)
	}
	return faults
}

// validateJournal is control 30: journal.max_rows ≥ 100.
//
// Below that a purge would erase the day's weighings, which are the only data of a
// station that cannot be rebuilt.
func (c *Config) validateJournal() []Fault {
	var faults faultList
	if c.Journal.MaxRows < 100 {
		faults.add("journal.max_rows", "%d est sous le plancher de 100 pesées conservées", c.Journal.MaxRows)
	}
	return faults
}

// validateAdminSecrets is control 31: admin.password_hash and
// admin.recovery_code_hash are USABLE when present.
//
// # Empty is not a fault, and that is a correction
//
// A station is installed WITHOUT a password: §14.4 says the delivered configuration
// is the export of §11.5, "qui ne porte aucun secret", and the first access is the
// recovery code printed on the installation sheet. Refusing an empty field put such
// a station OUT OF SERVICE (§11.3), so it could not weigh either — and weighing is
// the one thing it must do whatever else is wrong. What answers "aucun mot de passe
// n'est posé" is now the administration itself, which offers the recovery code.
//
// # What IS a fault: a hash nothing can match
//
// The delivered file carried « for-the-delivered-configurationg ». It parses, and its
// payload is EXACTLY the 32 bytes argon2id produces — so a length check would not
// have caught it either. It matches no password at all, `config validate` and
// `doctor` both declared it sound, and install.ps1, seeing a non-empty recovery
// field, skipped drawing a real code: the installation sheet went out blank and the
// station was locked out for good.
func (c *Config) validateAdminSecrets() []Fault {
	var faults faultList
	for _, secret := range []struct{ field, hash string }{
		{"admin.password_hash", c.Admin.PasswordHash},
		{"admin.recovery_code_hash", c.Admin.RecoveryCodeHash},
	} {
		switch {
		case secret.hash == "":
			// Documented state of a station between its installation and its first access.
		case !wellFormedArgon2id(secret.hash):
			faults.add(secret.field, "l'empreinte n'est pas une chaîne argon2id de la forme $argon2id$v=19$m=…,t=…,p=…$sel$empreinte")
		case !usableArgon2id(secret.hash):
			faults.add(secret.field, "l'empreinte est un remplissage : son corps est du texte, là où argon2id produit des octets tirés au sort — aucun mot de passe ne peut y correspondre")
		}
	}
	return faults
}

// validateCatalogShelving is controls 32 to 36: where a product lands, under which
// unique and legibly coloured category, and how often the source is looked at.
func (c *Config) validateCatalogShelving() []Fault {
	var faults faultList

	// 32. catalog.fallback_category belongs to the categories. It is what makes "the
	//     grid is empty because of an unexpected letter" impossible (§10.2 bis).
	categoryCodes := make([]string, 0, len(c.Catalog.Categories))
	present := make(map[string]bool, len(c.Catalog.Categories))
	for _, category := range c.Catalog.Categories {
		categoryCodes = append(categoryCodes, category.Code)
		present[category.Code] = true
	}
	if !present[c.Catalog.FallbackCategory] {
		faults.addChoice("catalog.fallback_category", categoryCodes,
			"%q ne désigne aucune catégorie : une lettre hors F/L/V/A n'aurait plus où atterrir",
			c.Catalog.FallbackCategory)
	}

	// 33. Category codes unique.
	seen := make(map[string]bool, len(c.Catalog.Categories))
	for i, category := range c.Catalog.Categories {
		if seen[category.Code] {
			faults.add(fmt.Sprintf("catalog.categories[%d].code", i), "le code %q est déclaré deux fois", category.Code)
		}
		seen[category.Code] = true
	}

	// 34. min_readable_ratio ∈ [0,1] -- the ABSOLUTE guard, on UNREADABLE rows
	//     (§10.4a).
	if ratio, ok := c.Catalog.Options.Ratio("min_readable_ratio"); ok && (ratio < 0 || ratio > 1) {
		faults.add("catalog.options.min_readable_ratio", "%v hors bornes [0, 1] : c'est une proportion de lignes lisibles", ratio)
	}

	// 35. Colours as #RRGGBB.
	for i, category := range c.Catalog.Categories {
		if !wellFormedColor(category.Color) {
			faults.add(fmt.Sprintf("catalog.categories[%d].color", i), "%q n'est pas une couleur #RRGGBB", category.Color)
		}
	}

	// 36. poll_interval_s ≥ 1. The stability check needs two consecutive polls, so a
	//     zero interval would read a file while the producer is still writing it.
	if interval, ok := c.Catalog.Options.Int("poll_interval_s"); ok && interval < 1 {
		faults.add("catalog.options.poll_interval_s", "%d est sous le plancher d'une seconde", interval)
	}
	return faults
}

// validateCatalogGuards is controls 39 and 40: what a drop path may point at, and
// how far a batch is allowed to shrink the shop.
func (c *Config) validateCatalogGuards(reg Registries) []Fault {
	var faults faultList

	// 39. No HTTP(S) host behind a DROP DIRECTORY (important-11). A source that declares
	//     one watches a directory it can list and delete from; one that demands an account
	//     and a password is a different source, with a different acknowledgement — and a
	//     "local" directory reached that way would be the Z: drive of the legacy
	//     application under another name.
	//
	//     The rule reads the SCHEMA and names no source. It used to be an `if` on
	//     `local_drop`, which was true only because `local_drop` was the only source that
	//     watched a directory; it now holds for the next one without this file being
	//     edited (ADR-052).
	//
	//     Its second half — « local_drop carries neither user nor password » — is GONE
	//     and not lost: control 9 already refuses a key the chosen source does not
	//     declare, and it now names the source that does. Two controls for one fact is how
	//     a third source ends up refused by the one nobody remembered to extend.
	for _, schema := range optionsUsedAs(reg.CatalogSources, c.Catalog.Type, UseDropDirectory) {
		if value, ok := c.Catalog.Options.Text(schema.Key); ok && isHTTPURL(value) {
			faults.addChoice("catalog.options."+schema.Key, sourcesFetchingByURL(reg.CatalogSources),
				"%q est un hôte HTTP(S) derrière un chemin de dépôt : c'est une source qui va chercher le fichier sur un partage qu'il faut choisir",
				value)
		}
	}

	// 40. max_weighable_drop ∈ [0, 0.5] -- the RELATIVE guard, on WEIGHABLE products
	//     (§10.4b, important-13).
	if drop, ok := c.Catalog.Options.Ratio("max_weighable_drop"); ok && (drop < 0 || drop > 0.5) {
		faults.add("catalog.options.max_weighable_drop", "%v hors bornes [0, 0,5] : c'est une baisse relative du nombre de produits pesables", drop)
	}
	return faults
}

// CheckPrice reports the fault a price carried by a delivered configuration file
// breaks, or nothing.
//
// It is control 43: every price carried by a DELIVERED configuration file verifies
// 0 ≤ price ≤ 999 999 cents -- the third and last imposition of MaxUnitPrice, with
// the DDL (§12.3) and the price rule of §10.3. Since §11.5 it is an ORDINARY
// configuration control, applied to a file like any other; it used to validate
// compiled values, that is, source code (ADR-026).
//
// It is the SINGLE implementation of that control, called by Config.Validate and by
// whoever loads the demonstration products and flv_demo.csv: three files, one rule,
// so that MaxUnitPrice cannot be enforced differently in three places.
func CheckPrice(field string, price Cents) []Fault {
	if price < 0 || price > MaxUnitPrice {
		return []Fault{{
			Field:   field,
			Message: fmt.Sprintf("%d hors bornes [0, %d] centimes", price, MaxUnitPrice),
		}}
	}
	return nil
}

// validateCatalogImages is controls 44 and 45: where the pictures come from, and
// how big one of them is allowed to be.
func (c *Config) validateCatalogImages(reg Registries) []Fault {
	var faults faultList

	// 44. catalog.images.source in the list, and path readable FROM THE CONTEXT OF THE
	//     SERVICE when the source is image_directory.
	if !known(imageSources(), c.Catalog.Images.Source) {
		faults.addChoice("catalog.images.source", imageSources(), "source d'images inconnue %q", c.Catalog.Images.Source)
	}
	if c.Catalog.Images.Source == ImageSourceDirectory {
		switch {
		case c.Catalog.Images.Path == "":
			// Empty is legitimate: it means <data>/product_images/, a directory the
			// service owns.
		case reg.Paths == nil:
			// No probe: we validate the form, we cannot validate the existence.
		default:
			if err := reg.Paths.Readable(c.Catalog.Images.Path); err != nil {
				faults.add("catalog.images.path", "%q n'est pas lisible depuis le contexte du service (%s)",
					c.Catalog.Images.Path, err)
			}
		}
	}

	// 45. max_image_size_kb ∈ [16, 4096] AND max_image_size_kb × 1024 ≤
	//     max_file_size_mb × 1 048 576: an image cannot be allowed to exceed the file
	//     that contains it (§10.7). The largest image really observed is 11 kB, the
	//     real file 527 kB.
	imageKB, hasImageKB := c.Catalog.Options.Int("max_image_size_kb")
	if hasImageKB && (imageKB < 16 || imageKB > 4096) {
		faults.add("catalog.options.max_image_size_kb", "%d ko hors bornes [16, 4096]", imageKB)
	}
	if fileMB, ok := c.Catalog.Options.Int("max_file_size_mb"); ok && hasImageKB {
		if imageKB*1024 > fileMB*1_048_576 {
			faults.add("catalog.options.max_image_size_kb",
				"%d ko dépasse le plafond du fichier qui la contient (%d Mo) : une image ne peut pas être plus grosse que son catalogue",
				imageKB, fileMB)
		}
	}
	return faults
}

// validateDropDirectory is control 46: a NAMED drop directory must be one the
// SERVICE can really work in (§10.1).
//
// Empty is the shipped case -- <data>/catalog/incoming, which the service owns and
// creates -- so there is nothing to probe. A nil probe means "we cannot know":
// `openscale config validate` on a laptop validates the form and not the existence,
// exactly like control 44 on catalog.images.path.
//
// Like 39 it reads the schema: WHICH key names a directory is the source's
// declaration, and the validation has no business holding a second copy of it.
func (c *Config) validateDropDirectory(reg Registries) []Fault {
	var faults faultList
	if reg.Paths == nil {
		return faults
	}
	for _, schema := range optionsUsedAs(reg.CatalogSources, c.Catalog.Type, UseDropDirectory) {
		directory, ok := c.Catalog.Options.Text(schema.Key)
		if !ok {
			continue
		}
		if named := strings.TrimSpace(directory); named != "" {
			if err := reg.Paths.Droppable(named); err != nil {
				faults.add("catalog.options."+schema.Key, "%s", err)
			}
		}
	}
	return faults
}

// 47. REMOVED, and its number left as a hole the way 37's was (ADR-044): §11.3 names
//
//	its controls by number, so renumbering what follows would falsify every reference
//	written elsewhere.
//
//	It said « a drop directory means nothing to a WebDAV share ». That was true, and it
//	was already what control 9 refuses — a key the chosen source does not declare — for
//	every source, present and to come. The only thing 47 added was its sentence, and that
//	sentence moved into control 9, which now NAMES the source that does declare the key.

// validateUpdate is control 48: update.repository is an owner/repo PAIR, never a URL.
//
// This is the only field of the file that says where privileged code will come from:
// the station downloads that repository's release and runs it as LocalSystem.
// Accepting a whole address here would make writing the configuration equivalent to
// running arbitrary code on the four stations. The host is compiled in; see
// UpdateConfig.
func (c *Config) validateUpdate() []Fault {
	var faults faultList
	if !repositoryShape.MatchString(c.Update.Repository) {
		faults.add("update.repository",
			"%q n'est pas un dépôt de la forme propriétaire/projet : ce champ ne prend pas d'adresse web",
			c.Update.Repository)
	}
	return faults
}

// validateGrid is control 49: ui.grid_columns is GridColumnsAutomatic, or a count
// between MinGridColumns and MaxGridColumns.
//
// The fault carries BOTH the range and the meaning of zero, because the two are of
// different natures and only one of them is a number of columns. Somebody who writes
// 1 is asking for a denser grid; if the refusal only named the interval, they would
// read « 1 est hors de [3, 12] » and never learn that the grid they had back is
// written 0 -- which looks, on a file, exactly like « aucune colonne ».
func (c *Config) validateGrid() []Fault {
	var faults faultList
	if c.UI.GridColumns != GridColumnsAutomatic &&
		(c.UI.GridColumns < MinGridColumns || c.UI.GridColumns > MaxGridColumns) {
		faults.addChoice("ui.grid_columns", gridColumnChoices(),
			"%d n'est pas un nombre de colonnes que la grille sait montrer", c.UI.GridColumns)
	}
	return faults
}

// validateChipThreshold is control 50: ui.min_products_for_chip is at least 1.
//
// A floor and no ceiling. What a ceiling would guard against -- a threshold above the
// biggest shelf, which leaves the category bar with « Tout » alone -- is undone by coming
// back to the field, and no pair of bounds is true of every catalogue: the same number is
// generous on the 331-weighable-tile export of 2026 and severe on the 107-tile one of
// 2022, and those are the same cooperative.
//
// The floor is the half that has no legitimate reading: under 1, a category with no tile
// at all would get a chip, and pressing it would open an empty grid. Zero never reaches
// here -- Config.UnmarshalJSON corrects it to DefaultMinProductsForChip, because a file
// that says nothing must not be refused (§11.2) -- so what this control catches is a
// negative somebody typed.
func (c *Config) validateChipThreshold() []Fault {
	var faults faultList
	if c.UI.MinProductsForChip < 1 {
		faults.add("ui.min_products_for_chip",
			"%d n'est pas un nombre de produits : à partir de 1, une catégorie obtient sa "+
				"puce dès qu'elle a ce nombre de tuiles sur ce poste",
			c.UI.MinProductsForChip)
	}
	return faults
}

// stabilityModes reports the two admissible values of stability.mode.
func stabilityModes() []string { return []string{ModeAdvisory, ModeBlocking} }

// timeoutActions reports the three admissible values of stability.on_timeout.
func timeoutActions() []string {
	return []string{OnTimeoutWarnAndPrint, OnTimeoutReject, OnTimeoutManualEntry}
}

// imageSources reports the three admissible values of catalog.images.source.
func imageSources() []string {
	return []string{ImageSourceCSV, ImageSourceDirectory, ImageSourceNone}
}

// gridColumnChoices reports what control 49 accepts, in the two natures the value
// has.
//
// It says a range in words where the other lists of this file enumerate values, and
// that is the point rather than an oversight: « Automatique » is not one more notch at
// the end of a slider, it is a different kind of answer, and a bare list of eleven
// numbers would spell zero like the other ten.
func gridColumnChoices() []string {
	return []string{
		fmt.Sprintf("%d — automatique : la grille suit l'écran, comme aujourd'hui", GridColumnsAutomatic),
		fmt.Sprintf("%d à %d — ce nombre de colonnes sur tous les écrans", MinGridColumns, MaxGridColumns),
	}
}
