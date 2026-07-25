package store

import (
	"context"
	"testing"

	"openscale/internal/domain"
)

// TestEveryRepositoryReportsInsteadOfPanickingOnAClosedBase is the store half of
// ADR-013 and of the graceful stop of §13.4: the journal degrades, the service never
// does. Workers are stopped in an order, and a write that arrives after Close must come
// back as an error the caller can log -- never a panic that takes the process down
// while a customer is at the scale.
//
// It also reaches the error branch of every query in the package, which is the only way
// to know those branches were written correctly rather than merely written.
func TestEveryRepositoryReportsInsteadOfPanickingOnAClosedBase(t *testing.T) {
	ctx := context.Background()
	db, _ := openAt(t, newClock(TestEpoch))
	seedCatalog(t, db, product("20", "AIL", "0493021000003", 532))
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	weighing := weighingOf("20", "AIL", "J-après-fermeture")
	decision := domain.LocalDecision{ProductID: "20", Offered: false, DecidedAt: TestEpoch}
	entry := TechnicalEntry{Level: LevelError, Source: LogSourceSystem, Message: "après fermeture"}
	imp := domain.Import{
		OccurredAt: TestEpoch, Source: domain.CatalogSourceLocalDrop, FileName: "flv_1.csv",
		SHA256: "sha", Result: domain.ImportFailed,
	}

	calls := map[string]func() error{
		"SchemaVersion":  func() error { _, err := db.SchemaVersion(); return err },
		"IntegrityCheck": func() error { return db.IntegrityCheck(ctx) },
		"Backup":         func() error { _, err := db.Backup(ctx); return err },
		"Vacuum":         func() error { return db.Vacuum(ctx) },

		"ReplaceCatalog": func() error {
			_, err := db.ReplaceCatalog(ctx, batch("flv_1.csv", "sha-2", TestEpoch,
				product("21", "PERSIL", "0493021100007", 400)))
			return err
		},
		"LoadCatalog": func() error { _, err := db.LoadCatalog(ctx); return err },
		"AllProducts": func() error { _, err := db.AllProducts(ctx); return err },
		"Product":     func() error { _, err := db.Product(ctx, "20"); return err },
		"Image":       func() error { _, err := db.Image(ctx, "abc"); return err },

		"RecordImport":      func() error { _, err := db.RecordImport(ctx, imp, nil); return err },
		"Imports":           func() error { _, err := db.Imports(ctx, 10, 0); return err },
		"LastAppliedImport": func() error { _, err := db.LastAppliedImport(ctx); return err },
		"Findings":          func() error { _, err := db.Findings(ctx, 1); return err },

		"RecordContentFailure": func() error {
			_, err := db.RecordContentFailure(ctx, "sha", "ERR-CAT-03", "illisible")
			return err
		},
		"Quarantine":        func() error { _, err := db.Quarantine(ctx, "sha"); return err },
		"QuarantineEntries": func() error { _, err := db.QuarantineEntries(ctx); return err },
		"ForgetQuarantine":  func() error { _, err := db.ForgetQuarantine(ctx, ""); return err },

		"SaveDecision":   func() error { return db.SaveDecision(ctx, decision) },
		"ClearDecision":  func() error { return db.ClearDecision(ctx, "20") },
		"Decision":       func() error { _, err := db.Decision(ctx, "20"); return err },
		"LocalDecisions": func() error { _, err := db.LocalDecisions(ctx); return err },

		"RecordWeighing":  func() error { return db.RecordWeighing(ctx, &weighing) },
		"PurgeWeighings":  func() error { _, err := db.PurgeWeighings(ctx); return err },
		"CountWeighings":  func() error { _, err := db.CountWeighings(ctx); return err },
		"Weighings":       func() error { _, err := db.Weighings(ctx, JournalFilter{}); return err },
		"WeighingByJobID": func() error { _, err := db.WeighingByJobID(ctx, "J-1"); return err },

		"RecordTechnical":  func() error { return db.RecordTechnical(ctx, entry) },
		"PurgeTechnical":   func() error { _, err := db.PurgeTechnical(ctx); return err },
		"TechnicalEntries": func() error { _, err := db.TechnicalEntries(ctx, TechnicalFilter{}); return err },
		"CountTechnical":   func() error { _, err := db.CountTechnical(ctx); return err },

		"Meta":    func() error { _, _, err := db.Meta(ctx, MetaLabelsSinceRoll); return err },
		"SetMeta": func() error { return db.SetMeta(ctx, MetaLabelsSinceRoll, "1") },
		"AddMeta": func() error { _, err := db.AddMeta(ctx, MetaLabelsSinceRoll, 1); return err },
		"MetaAll": func() error { _, err := db.MetaAll(ctx); return err },
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s a paniqué sur une base fermée : %v", name, r)
				}
			}()
			if err := call(); err == nil {
				t.Fatalf("%s a réussi sur une base fermée", name)
			}
		})
	}
}
