package domain

// Locale names the language the label prints its OWN words in: the unit after a
// weight, the singular and plural of a count of items, the currency symbol.
//
// It carries nothing else. The amounts and the masses are already formatted by
// Cents.Euro and Grams.Kilos, whose decimal comma is not a language setting but the
// way the till and the customer read a price in this country; and every message the
// application says out loud is French by decision, not by configuration.
//
// It is a plain string rather than an enumeration because it travels through
// ports.PrintJob and through the configuration file, where an operator may one day
// write something this binary has never heard of. The renderer answers that by
// falling back to French AND journalling it, never by refusing to print a label a
// customer is standing there waiting for.
type Locale string

// LocaleFrench is the one locale V1 ships, and the value the empty locale means.
//
// A second one is a table of words, not a change of design -- which is exactly why
// the type exists before there is a second language to put in it.
const LocaleFrench Locale = "fr"
