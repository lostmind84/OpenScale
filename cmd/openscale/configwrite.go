package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"openscale/internal/domain"
	"openscale/internal/platform"
	"openscale/internal/web"
)

// This file holds the three `config` actions that REWRITE the station's own file —
// migrate, password and recovery-code. All three go through the store the
// administration screen saves with, so a change made from a terminal rotates the five
// versions and lands atomically like any other (§11.4), and the station does not see
// it before it restarts.

// editConfigFile reads the station's configuration, hands it to change, and writes it
// back.
//
// The two callers below each set ONE field of the administration block, and each used
// to carry its own copy of the open, the read and the write. What SURROUNDS the change
// is gathered here so that the third caller somebody writes later cannot forget it: the
// store, which is what rotates config.json.1 … .5 and lands the file atomically (§11.4),
// and the instant that dates the file.
//
// NOTHING reaches disk when change refuses. A password that could not be hashed, or a
// line nobody typed, leaves the file exactly as it was.
func editConfigFile(path string, change func(cfg *domain.Config) error) error {
	store, err := platform.NewConfigStore(path)
	if err != nil {
		return err
	}
	ctx := context.Background()
	cfg, err := store.Read(ctx)
	if err != nil {
		return errors.New(readFailure(path, err))
	}
	if err := change(&cfg); err != nil {
		return err
	}
	cfg.ModifiedAt = platform.NewSystemClock().Now()
	return store.Save(ctx, cfg)
}

// setAdminPassword is `openscale config password` (§14.4).
//
// It writes the FILE, through the same store the administration screen saves with, so a
// password set from a terminal rotates the versions and lands atomically like any other
// change. The station does not see it before it restarts: nothing re-reads config.json
// while the service runs, and pretending otherwise would have somebody typing a password
// that works only after the next power cut.
func setAdminPassword(in io.Reader, out io.Writer, path string) error {
	// Read inside the change, because the last sentence of the command depends on it: a
	// station with no recovery code has one gesture left to make.
	var withoutRecoveryCode bool
	err := editConfigFile(path, func(cfg *domain.Config) error {
		// web.MinPasswordLength is the AUTHORITY, here as on the recovery form of §14.4:
		// this command holds no floor of its own, it applies the station's.
		fmt.Fprintf(out, "Nouveau mot de passe d'administration pour %s\n"+
			"(au moins %d caractères, il s'affiche à l'écran) : ", path, web.MinPasswordLength)
		typed, err := readSecretLine(in)
		if err != nil {
			return err
		}
		if len([]rune(typed)) < web.MinPasswordLength {
			return fmt.Errorf("le mot de passe doit faire au moins %d caractères",
				web.MinPasswordLength)
		}

		hash, err := web.HashSecret(typed)
		if err != nil {
			return err
		}
		cfg.Admin.PasswordHash = hash
		withoutRecoveryCode = cfg.Admin.RecoveryCodeHash == ""
		return nil
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "\nMot de passe d'administration posé dans %s.\n", path)
	if withoutRecoveryCode {
		fmt.Fprintf(out, "Ce poste n'a AUCUN code de secours : sans lui, ce mot de passe "+
			"perdu se rattrape uniquement ici. Tirez-en un avec « openscale config "+
			"recovery-code » et recopiez-le sur la fiche d'installation.\n")
	}
	fmt.Fprintf(out, "Redémarrez le service pour que le poste le prenne en compte.\n")
	return nil
}

// mintRecoveryCode is `openscale config recovery-code` (§14.4, important-10).
//
// The code is shown ONCE, in clear, and never again: the configuration keeps its argon2id
// hash and nothing else. Whoever runs this command has one job left, and it is written on
// the last line — copy the eight characters onto the installation sheet, which goes into
// the shop's folder and not onto the station.
func mintRecoveryCode(out io.Writer, path string) error {
	var (
		code      string
		replacing bool
	)
	err := editConfigFile(path, func(cfg *domain.Config) error {
		replacing = cfg.Admin.RecoveryCodeHash != ""

		minted, err := web.NewRecoveryCode()
		if err != nil {
			return err
		}
		hash, err := web.HashSecret(minted)
		if err != nil {
			return err
		}
		cfg.Admin.RecoveryCodeHash = hash
		code = minted
		return nil
	})
	if err != nil {
		return err
	}

	if replacing {
		fmt.Fprintf(out, "L'ancien code de secours de ce poste ne fonctionne plus : "+
			"la fiche déjà classée est à corriger.\n")
	}
	fmt.Fprintf(out, "Code de secours de ce poste : %s\n", code)
	fmt.Fprintf(out, "Recopiez-le sur la fiche d'installation MAINTENANT : il ne sera "+
		"plus jamais affiché.\n")
	return nil
}

// readSecretLine reads ONE line off the standard input.
//
// No echo suppression, and it is a deliberate refusal rather than an oversight: turning
// the terminal echo off means a terminal package, which means a seventh module in a
// perimeter §17.1 closes at six, bought for the one call it would take — ADR-039 weighs
// a dependency on the surface actually called, and this one is a single call. The usage
// text says the line is visible, and the machine it is typed on is the station's own
// console.
func readSecretLine(in io.Reader) (string, error) {
	if in == nil {
		return "", errors.New("aucune entrée standard : le mot de passe se tape au clavier, " +
			"ou s'envoie par un tube")
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("lecture du mot de passe : %w", err)
	}
	// Typed on a Windows console the line ends with \r\n, piped from a file it may end
	// with nothing at all. Only those two characters go: a password is allowed to end
	// with a space, and trimming it would refuse tomorrow what it accepted today.
	//
	// The byte order mark at the HEAD goes too, and it is the one character nobody typed:
	// piping into a native process from a PowerShell console in chcp 65001 puts EF BB BF in
	// front of the standard input. Hashing it walls the station off — what the volunteer
	// types back, on the screen or on this same console, is the password WITHOUT the mark
	// and would never verify — and nothing on either end would show why.
	return strings.TrimPrefix(strings.TrimRight(line, "\r\n"), "\ufeff"), nil
}

// migrateConfig is `openscale config migrate`.
//
// It writes through the same store the administration screen saves with, so a migration
// rotates config.json.1 … .5 and lands atomically like any other change. Nothing new is
// invented for it, and that is the point: the version of before is one file away.
//
// It is IDEMPOTENT. update.ps1 and update.sh call it at every update, and a station that is
// already at this schema must come out of it with its file untouched -- rotating five
// versions over a no-operation is how the version that mattered falls off the end.
//
// A refused point suspends the WHOLE write, not only its own key: what could be carried
// stays computed, correctly, in the cfg this run holds in memory, but nothing at all
// reaches disk while a single point is still refused -- see the comment on the refusal
// branch below for why that has to hold even for a file that carries one point migrate CAN
// write and one it cannot.
//
// A block that would not DECODE suspends it the same way, and that one is not a migration
// question at all: what this command holds for such a block is the neutral profile, and
// rewriting the file would post the factory value over whatever the shop had declared.
func migrateConfig(out io.Writer, path string) error {
	cfg, notes, decodeFaults, err := platform.LoadConfig(path)
	if err != nil {
		return fmt.Errorf("le fichier de configuration %s ne peut pas être lu : %w", path, err)
	}

	// A key control 20 refuses outright earns NO note of its own when Migrate has no
	// translation to attempt for it: the six keys of the numbering plan are a pure
	// refusal, unchanged since they entered the code already retired (configmigration.go),
	// and migrationSteps never touches them. cfg.Retired() is the only place that survives
	// for them, and it is also what ConfigStore.Save is about to consult -- so a key still
	// there is folded into the very same accounting as the notes, under the same name a
	// note would use, before Save is ever called.
	retired := cfg.Retired()
	named := make(map[string]bool, len(notes))
	for _, note := range notes {
		named[note.Key] = true
	}
	for _, key := range retired {
		if named[key] {
			continue
		}
		notes = append(notes, domain.MigrationNote{
			Key: key, Action: domain.MigrationRefused, Message: domain.RetiredKeyReason(key),
		})
	}

	if len(notes) == 0 && len(decodeFaults) == 0 {
		fmt.Fprintf(out, "%s est déjà à la forme que ce binaire lit : rien à faire.\n", path)
		return nil
	}

	refused := 0
	if len(notes) > 0 {
		fmt.Fprintf(out, "%s : %d changement(s).\n", path, len(notes))
		for _, note := range notes {
			fmt.Fprintf(out, "  %s\n", note)
			if note.Action == domain.MigrationRefused {
				refused++
			}
		}
	}

	// ConfigStore.Save calls cfg.RefuseIfRetired, and a MIXED file -- one point carried,
	// one refused -- would reach it and be refused there too, only AFTER this command had
	// already said it was writing. Leaving here, before Save is ever called, is what keeps
	// « rien n'est écrit » true unconditionally, whether the refusal came with a note (an
	// unconvertible discount, a file from a newer binary) or without one (the numbering
	// plan, folded in just above).
	if refused > 0 {
		return &serviceFailure{Exit: exitFailure, Message: fmt.Sprintf(
			"%s comporte %d point(s) que ce binaire ne devine pas : le fichier n'est pas "+
				"modifié, tranchez-le puis relancez la migration", path, refused)}
	}

	// A block that would not decode is the SAME suspension, for a worse reason. Block-by-
	// block decoding replaces it with the one of the neutral profile so that the station
	// still serves its fault list -- but that substitute is a plausible factory value
	// NOBODY DECLARED, and writing it back would make it the shop's own. Measured on the
	// delivered file with an unreadable `pricing` block: the members' 10 % discount
	// disappeared, the command announced one unrelated change and exited 0, and update.ps1
	// runs it on its own after every successful update.
	if len(decodeFaults) > 0 {
		for _, fault := range decodeFaults {
			fmt.Fprintf(out, "  %s\n", fault.String())
		}
		return &serviceFailure{Exit: exitFailure, Message: fmt.Sprintf(
			"%s : le fichier n'est pas modifié, le réécrire poserait la configuration d'usine "+
				"%s. Corrigez-le, puis relancez la migration",
			unreadablePart(path, decodeFaults),
			(&domain.UnreadableBlocksError{Faults: decodeFaults}).InTheirPlace())}
	}

	store, err := platform.NewConfigStore(path)
	if err != nil {
		return err
	}
	cfg.ModifiedAt = platform.NewSystemClock().Now()
	if err := store.Save(context.Background(), cfg); err != nil {
		return fmt.Errorf("%s n'a pas pu être réécrit : %w", path, err)
	}
	fmt.Fprintf(out, "%s réécrit ; la version d'avant est dans %s.1.\n", path, path)
	fmt.Fprintf(out, "Redémarrez le service pour qu'il lise le fichier réécrit.\n")
	return nil
}
