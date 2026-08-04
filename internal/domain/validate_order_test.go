package domain

import "testing"

// TestValidateReportsItsFaultsInTheOrderTheControlsAreNumbered holds the SEQUENCE
// Config.Validate returns, and not merely its contents.
//
// # Why the order is a contract and not a detail
//
// The slice this test freezes is displayed, in this order, to a human being:
// `openscale doctor` prints it, the administration screen lists it, `openscale
// config validate` writes it to a terminal, and a station whose configuration was
// refused shows « Poste en configuration d'usine (ERR-CFG-01) » with these fields
// under it. A volunteer reads that list top to bottom, over the telephone, on a bad
// morning. §11.3 also names its controls BY NUMBER -- « control 20 », « control 46 »
// -- so the order the faults come out in is what a screen, a test and a paragraph of
// the architecture all agree on.
//
// Nothing else in this package holds it. Every other test looks a fault up by its
// field (findFault), which passes just as happily when two groups of controls have
// swapped places: that was measured on 03/08/2026 by interverting controls 22-25
// with 26-28, and the whole suite stayed green.
//
// # If you are reading this because the test is red
//
// Ask one question: did you MEAN to change the order faults come out in?
//
//   - If you added a control, it belongs at the end of the numbering (§11.3 leaves
//     37 and 47 as holes rather than renumbering), and its field belongs at the
//     matching place in the lists below. Add it and move on.
//   - If you moved an existing control, or reordered the calls in Config.Validate,
//     that is the change this test exists to stop. The numbering is published; a
//     station and a document that disagree about which control is 46 cost more than
//     the tidiness gained.
//
// The two cases below are deliberately different in nature. The first breaks thirty-one
// fields at once and walks the whole numbering from control 1 to control 50. The
// second is the one a lookup by field could never hold: printer.options.transport is
// reported THREE times, by controls 7, 8 and 42, and only its POSITION says which.
func TestValidateReportsItsFaultsInTheOrderTheControlsAreNumbered(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		break_ func(*Config, *Registries)
		want   []string
	}{
		{
			name: "trente-et-un champs cassés, du contrôle 1 au contrôle 50",
			break_: func(c *Config, _ *Registries) {
				c.Station.Number = 0                                      // 1
				c.Network.Listen = "pas une adresse"                      // 2
				c.Scale.Type = "gram-xfoc-turbo"                          // 3
				c.Printer.Type = ""                                       // 4
				c.Catalog.Type = CatalogSourceManual                      // 5
				c.Pricing.PrimaryCode = "GHOST"                           // 14
				c.Pricing.ReferenceCode = "GHOST2"                        // 15
				c.Pricing.SecondaryCodes = []string{"NOPE"}               // 16
				c.Limits.BasketMin, c.Limits.BasketMax = 50, 100          // 22
				c.Limits.MinWeight, c.Limits.MaxWeight = 9000, 10         // 23
				c.Limits.MinUnits, c.Limits.MaxUnits = 5, 500             // 24
				c.Limits.MaxAmount = 999_999                              // 25
				c.Stability.Timeout = 0                                   // 26
				c.Stability.ExpiryFloor, c.Stability.ExpiryCeiling = 1, 0 // 27
				c.Stability.Mode = "bloquant"                             // 28
				c.Stability.OnTimeout = "refuser"                         // 28
				c.Printer.Template = "inconnu"                            // 29
				c.Journal.MaxRows = 1                                     // 30
				// 31, twice: a hash that parses but matches nothing, then one that does
				// not even parse.
				c.Admin.PasswordHash = "$argon2id$v=19$m=65536,t=3,p=2$b3BlbnNjYWxlLXNhbHQxMg$Zm9yLXRoZS1kZWxpdmVyZWQtY29uZmlndXJhdGlvbmc"
				c.Admin.RecoveryCodeHash = "pas-du-tout-argon2id"
				c.Catalog.FallbackCategory = "divers"                       // 32
				c.Catalog.Categories[1].Code = c.Catalog.Categories[0].Code // 33
				c.Catalog.Categories[0].Color = "vert"                      // 35
				c.Catalog.Images.Source = "jpeg"                            // 44
				c.Update.Repository = "https://exemple.test/depot"          // 48
				c.UI.GridColumns = 1                                        // 49
				c.UI.MinProductsForChip = -1                                // 50
			},
			want: []string{
				"station.number",              // 1
				"network.listen",              // 2
				"scale.type",                  // 3
				"printer.type",                // 4
				"catalog.type",                // 5
				"pricing.primary_code",        // 14
				"pricing.reference_code",      // 15
				"pricing.secondary_codes[0]",  // 16
				"limits.basket_min_g",         // 22
				"limits.max_weight_g",         // 23
				"limits.max_units",            // 24
				"limits.max_amount_cents",     // 25
				"stability.timeout_ms",        // 26
				"stability.expiry_floor_ms",   // 27
				"stability.expiry_ceiling_ms", // 27
				"stability.mode",              // 28
				"stability.on_timeout",        // 28
				"printer.template",            // 29
				"journal.max_rows",            // 30
				"admin.password_hash",         // 31
				"admin.recovery_code_hash",    // 31
				"catalog.fallback_category",   // 32
				"catalog.categories[1].code",  // 33
				"catalog.categories[0].color", // 35
				"catalog.images.source",       // 44
				"update.repository",           // 48
				"ui.grid_columns",             // 49
				"ui.min_products_for_chip",    // 50
			},
		},
		{
			name: "les options et les chemins, où un même champ est nommé trois fois",
			break_: func(c *Config, reg *Registries) {
				reg.Paths = unreadablePaths{}
				c.Catalog.Type = CatalogSourceLocalDrop
				c.Catalog.Images.Source = ImageSourceDirectory
				c.Catalog.Images.Path = `Z:\images`
				c.Catalog.Options = c.Catalog.Options.WithText("directory", "https://nas.test/depot")
				c.Catalog.Options = c.Catalog.Options.WithText("inconnue", "x")
				c.Printer.Options = c.Printer.Options.WithText("transport", "rs232")
				c.Printer.Options = c.Printer.Options.WithText("queue_bis", "x")
			},
			want: []string{
				"printer.options.transport", // 7, the schema of the raster driver
				"printer.options.queue_bis", // 7, a key no driver declares
				"printer.options.transport", // 8, not a registered transport
				"catalog.options.inconnue",  // 9
				"catalog.options.password",  // 9, declared by webdav and not by local_drop
				"catalog.options.url",       // 9
				"catalog.options.username",  // 9
				"catalog.options.directory", // 39, an HTTP host behind a drop path
				"printer.options.transport", // 42, a serial transport for the printer
				"catalog.images.path",       // 44, unreadable from the service
				"catalog.options.directory", // 46, the service cannot write there
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			config := loadDelivered(t)
			registries := testRegistries()
			testCase.break_(&config, &registries)

			got := config.Validate(registries)
			fields := make([]string, len(got))
			for i, fault := range got {
				fields[i] = fault.Field
			}
			// The FIRST divergence is reported and the comparison stops there. One
			// control moved out of place shifts every fault after it, so listing all
			// of them would bury the one line that says where the sequence broke.
			for i := range fields {
				if i >= len(testCase.want) {
					t.Fatalf("faute n° %d en trop : %q\nobtenu  %v\nattendu %v",
						i+1, fields[i], fields, testCase.want)
				}
				if fields[i] != testCase.want[i] {
					t.Fatalf("faute n° %d : %q, attendu %q\nobtenu  %v\nattendu %v",
						i+1, fields[i], testCase.want[i], fields, testCase.want)
				}
			}
			if len(fields) < len(testCase.want) {
				t.Fatalf("faute n° %d manquante : %q\nobtenu  %v\nattendu %v",
					len(fields)+1, testCase.want[len(fields)], fields, testCase.want)
			}
		})
	}
}
