package domain

// ONE COMPILED PROFILE, AND ONLY ONE (ADR-026).
//
// LaCagetteProfile() does not exist, and the reason matters more than the absence.
// It was the reflex of the SystemeDefaut table of the legacy application -- 227
// columns, one row -- and of its ENREGISTRER / RESTAURER LES VALEURS PAR DÉFAUT
// buttons: the reference configuration lived INSIDE the application and was
// "restored" into the effective one. The legacy application did that because it
// could not copy a file. This architecture has a config.json that copies, exports
// and imports, with a field-by-field diff and a fingerprint, and it would have
// reintroduced a reference locked inside the binary anyway.
//
// What that removal buys, concretely: changing a price coefficient or the URL of
// the share is no longer a RECOMPILATION followed by a redeployment on four
// stations; there are no longer TWO sources of truth for the default values (the Go
// code and the files); the binary becomes a PRODUCT again instead of a bespoke
// executable for one customer; and control 43, whose object was to validate source
// code, becomes an ordinary configuration control.
//
// The values of La Cagette are a DELIVERED FILE: config-lacagette.json, versioned
// in the release archive with its fingerprint, copied by the installer and
// replayable through the import path that already exists.

// NeutralProfile is the only compiled profile: mono-tarif, generic safeguards,
// basket check off, printer preview, scale.present false.
//
// It is reduced to the STRICT MINIMUM THAT LETS THE PROCESS START AND SHOW ITS
// ADMINISTRATION SCREEN -- exactly the part it plays when a configuration is
// invalid (§11.3): the server loads it IN MEMORY WITHOUT WRITING, serves the list
// of faults and shows a full-screen « Poste en configuration d'usine (ERR-CFG-01) ».
// The station always starts; a broken configuration must never produce a black
// screen.
//
// IT CONTAINS NO URL, NO PRICE COEFFICIENT AND NO THRESHOLD MEASURED AT A
// CUSTOMER'S SITE. It names no piece of hardware either: scale.type is empty
// because a station that declares it has no scale has no protocol to name, and the
// printer is preview, which needs neither a queue nor a device node.
//
// VALIDATE REPORTS EXACTLY ONE FAULT ON IT, admin.password_hash, and that is
// deliberate rather than an oversight: a virgin station has no password, control 31
// says so, and the answer to that single fault is step 1 of the first-start wizard
// (§14.4) -- which is the same answer §11.5 requires of an import into a station
// without a password. A profile that validated clean would be a profile whose
// administration screen writes configuration unprotected.
func NeutralProfile() Config {
	return Config{
		Version: 1,
		Readme: "Modifiable depuis l'écran d'administration. Édition manuelle : arrêtez le " +
			"service, éditez, redémarrez. Copies de secours en config.json.1 à .5.",
		// ModifiedAt stays zero: the clock is injected, and nothing in
		// internal/domain reads one. Whoever writes this profile to disk stamps it.

		Station: StationConfig{Number: 1},
		Network: NetworkConfig{Listen: "127.0.0.1:8085"},

		UI: UIConfig{
			Language:             "fr",
			Sound:                true,
			IdleTimeoutSeconds:   45,
			ReprintWindowSeconds: 60,
			ShowGridPrices:       true,
		},

		Scale: ScaleConfig{
			// No protocol, and no serial options with it: this station declares it has
			// no scale, so manual entry is NOMINAL and the light goes off instead of
			// staying red for ever (§9.3).
			Present:             false,
			ManualEntryAllowed:  true,
			DegradeAfterSeconds: 20,
		},

		Printer: PrinterConfig{
			// preview writes a life-size PDF or a PNG: it needs no queue, no device
			// node and no calibrated roll, so a station in factory configuration can
			// show its administration screen and even produce a label to look at.
			Type: PrinterPreview,
			// The template of the neutral profile is the one whose boxes PLACE ink
			// rather than reproduce a label: it satisfies the nine hard rules on a
			// single-tier grid.
			Template: "weighing_neutral_single",
		},

		// Mono-tarif: one tier, no discount, and the label prints one price. The
		// secondary field disappears through its own condition, with no `if` in the
		// renderer.
		Pricing: SingleTierRules(),

		Barcode: BarcodeConfig{VerifyReferenceCheckDigit: true},

		// GENERIC safeguards: the capacity of the barcode fields and the plain
		// arithmetic of a scale, never a figure measured in a shop. The basket check is
		// OFF, because -282 g is the tare of one cooperative's basket and nothing else.
		Limits: WeighingLimits{
			EmptyMax:           5,
			BasketCheckEnabled: false,
			MinWeight:          10,
			MaxWeight:          MaxWeight, // the capacity of the NNDDD field
			MaxTare:            9_999,
			MinUnits:           1,
			MaxUnits:           99, // two digits of payload on the by-unit prefix
			MaxAmount:          99_999,
		},

		// advisory, so that the day this application replaces the old one, no weighing
		// can be refused for a reason the old one never checked (A3).
		Stability: DefaultStabilityPolicy(),

		Catalog: CatalogConfig{
			// local_drop and NOT webdav: a neutral profile carries no URL, no account
			// and no password. The service creates the directory itself.
			Type: CatalogSourceLocalDrop,
			// The images of the reference exchange file travel INSIDE it, and the format
			// is read from the header bytes rather than from the extension (§10.7).
			Images: ImagesConfig{Source: ImageSourceCSV},
			// The four shelves of the Odoo adapter must exist, and a fallback among
			// them: a letter outside F/L/V/A is a defect of the file, never an empty
			// grid (§10.2 bis).
			FallbackCategory: "other",
			Categories: []Category{
				{Code: "fruits", Label: "Fruits", Rank: 1, Color: "#C0392B", Visible: true},
				{Code: "vegetables", Label: "Légumes", Rank: 2, Color: "#27AE60", Visible: true},
				{Code: "bulk", Label: "Vrac", Rank: 3, Color: "#B7950B", Visible: true},
				{Code: "other", Label: "Autres", Rank: 4, Color: "#5D6D7E", Visible: true},
			},
		},

		Journal: JournalConfig{MaxRows: 5_000, MaxDays: 90, MaxTechnical: 2_000},

		// No password: a virgin station has none, and the first-start wizard imposes
		// one before anything may write a configuration (§11.5, ADR-018).
		Admin: AdminConfig{SessionMinutes: 30, AttemptsPerMinute: 5},

		Maintenance: MaintenanceConfig{WeeklyIntegrityCheck: true, DiskAlertMB: 200},
	}
}
