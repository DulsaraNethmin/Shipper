package notifications

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The redaction rules (SHIP-141): what a notification may not say.
//
// Docs/01 §4.4 and this ticket's *Done when*: no address, no goods description and no full customer
// name in a notification body. A push renders in front of whoever is holding the handset, which is a
// display nobody consented to — the person reading it may not be the person the message is for.
//
// # There are two guards here and they catch different things, which is the whole design
//
// **The structural guard came first and is the stronger of the two.** [content] has two fields,
// neither of them from the event payload, and [facts] — everything this domain reads out of an
// event — has six, none of which is an address, a description or a name. A renderer cannot leak
// what it was never handed, and text/template turns that into a compile-time-ish guarantee: a
// template referencing a field the struct does not have fails to execute. `TestTheClosedInputIsStillClosed`
// and `TestTheFactsThisDomainReadsCarryNothingToLeak` hold both shapes by reflection, so widening
// either is a decision somebody has to argue rather than a field somebody adds.
//
// **The word-level guard is this file, and it exists because wave 10 proved the structural one is
// not sufficient.** [Rule.Headline] is free prose typed into rules.go. Nothing about it is closed:
// it is a Go string literal, a later ticket edits it, and the reviewer of that edit is the only
// thing between a sentence and a handset. Wave 10 isolated the sentence *"The customer has set a
// maximum."* — no field, no value, prose alone — and a thirteen-test suite passed with it live.
// **A test that asserts on a field name does not catch a leak written as prose.**
//
// # What the word-level guard refuses, and why each rule is the shape it is
//
// It runs over the *rendered* text with the job identifier removed, so it sees what a handset shows
// rather than a template source.
//
//  1. **Any digit.** A street number, a unit number, a postcode, a weight, a quantity, a phone
//     number and a dollar amount are all digits, and none of them belongs in this copy. It is the
//     single most effective rule here precisely because it is not a vocabulary: it does not have to
//     know what a postcode looks like. The job identifier is removed first because it is the one
//     number a notification is allowed to carry — and it is meaningless to anybody who cannot
//     already open the job, which is what makes it safe on a locked screen.
//
//  2. **A capitalised word that does not start a sentence**, outside a two-word allowlist. This is
//     the rule that catches a person: "Your job for John Smith is ready" has no field name in it,
//     no digits, and no address vocabulary. It also catches a suburb, a street name and a business
//     name, which is most of what an address is once the number has gone.
//
//  3. **A street type**, case-insensitively, from a closed list. Australia Post publishes the set,
//     so unlike a goods vocabulary it is genuinely closed. This is the rule that catches an address
//     somebody wrote in lower case, which rules 1 and 2 would both miss.
//
//  4. **An `@`, a URL or a `www.`** — an email address, and a link that would make a push a
//     phishing surface.
//
// # What it cannot catch, said plainly rather than left to be discovered
//
// **A goods description written in lower case with no digits.** "two pallets of copper piping" trips
// none of the four rules, and no textual rule can distinguish it from ordinary prose. A blocklist of
// goods words would be endlessly incomplete and — worse — would invite the belief that it was not.
// That half of the *Done when* is met **structurally and only structurally**: no description exists
// in [facts], so none exists in [content], so no template can reference one. The same is true of a
// name written in lower case.
//
// The division is deliberate: the structural guard covers everything that would have to arrive
// through the event, and the word-level guard covers everything somebody can type into rules.go.
// Between them there is no path by which an address reaches a body, and each half is weak exactly
// where the other is strong.
//
// # False positives fail the build, and that is the safe direction
//
// "drive", "court", "lane" and "terrace" are ordinary English words as well as street types, so copy
// that uses one is refused. The cost is a reworded sentence caught by a failing test; the cost of
// the opposite default is a street address on a stranger's lock screen. Whoever meets one should
// reword rather than widen the list — and if the list genuinely has to change, this comment is where
// the argument goes.

// allowedCapitals are the capitalised words that may appear mid-sentence in notification copy.
//
// Two, and the point of the list is that it is two. `Shipper` is the platform's own name and appears
// in "the Shipper app" and in the email sign-off; `Job` appears in the push and SMS bodies, where it
// labels the identifier. Everything else capitalised mid-sentence is a person, a place or a
// business, which is the failure this rule exists to catch.
//
// **Adding a word here is a privacy decision, not a copy one.** The question to answer is what stops
// the new word being somebody's surname.
var allowedCapitals = map[string]bool{
	"Shipper": true,
	"Job":     true,
}

// streetTypes is the closed vocabulary rule 3 refuses, in full and in abbreviation.
//
// Drawn from the Australian Postal Corporation's street-type list, which is what an address in this
// country is actually built from. Only the unambiguous entries and their standard abbreviations are
// here: `way`, `close`, `rise`, `view`, `grove` and `quay` are all ordinary English and all
// deliberately absent, because an address using one still carries a street number (rule 1) and a
// capitalised street name (rule 2).
var streetTypes = map[string]bool{
	"street": true, "st": true,
	"road": true, "rd": true,
	"avenue": true, "ave": true,
	"drive": true, "dr": true,
	"lane": true, "ln": true,
	"court": true, "ct": true,
	"place": true, "pl": true,
	"parade": true, "pde": true,
	"highway": true, "hwy": true,
	"crescent": true, "cres": true,
	"terrace": true, "tce": true,
	"boulevard": true, "blvd": true,
	"circuit": true, "cct": true,
	"esplanade": true, "esp": true,
}

var (
	// jobIdentifier is the one value a notification may carry that has digits in it.
	//
	// Removed before anything else runs, so that rule 1 can be "no digits at all" rather than "no
	// digits except these", which is the version somebody eventually widens.
	jobIdentifier = regexp.MustCompile(
		`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

	// wordPattern is a run of letters, with the apostrophes and hyphens English words contain.
	//
	// Punctuation is deliberately outside it: `(job` and `awarded.` have to reduce to `job` and
	// `awarded` for the capitalisation rule to be about words rather than about typography.
	wordPattern = regexp.MustCompile(`\pL[\pL'’\-]*`)

	// linkPattern catches the two shapes a link takes in prose. A push notification carrying a
	// link is a phishing surface as well as a disclosure, which is why it is here rather than in
	// a separate check.
	linkPattern = regexp.MustCompile(`(?i)(https?://|www\.)`)
)

// Redactions is what a piece of notification copy must not contain, as a list of problems.
//
// Empty means clean. Each entry names the rule and quotes what tripped it, because a redaction
// failure is read by whoever wrote the copy and "this text is not allowed" is not something they can
// act on.
//
// It takes the **rendered** text rather than a template source: what matters is what a handset
// shows, and a template that assembles a forbidden phrase out of permitted parts would pass a check
// on its source. The job identifier is removed first — see [jobIdentifier].
func Redactions(text string) []string {
	stripped := jobIdentifier.ReplaceAllString(text, "")

	var problems []string

	// 1. Any digit. The job identifier is gone by now, so there is nothing legitimate left.
	if i := strings.IndexFunc(stripped, unicode.IsDigit); i >= 0 {
		problems = append(problems, "it contains a digit ("+quoteAround(stripped, i)+"); a street "+
			"number, a unit, a postcode, a weight, a quantity and an amount are all digits, and "+
			"the job identifier is the only number a notification may carry")
	}

	// 4. Links and email addresses, checked on the whole text rather than word by word.
	if strings.Contains(stripped, "@") {
		problems = append(problems, "it contains an `@`, which in this copy is an email address")
	}
	if match := linkPattern.FindString(stripped); match != "" {
		problems = append(problems, "it contains a link ("+match+"); a push notification carrying "+
			"one is a phishing surface as well as a disclosure")
	}

	// 2 and 3, over the words.
	for _, span := range wordPattern.FindAllStringIndex(stripped, -1) {
		word := stripped[span[0]:span[1]]

		if streetTypes[strings.ToLower(word)] {
			problems = append(problems, "it contains the street type "+strconv.Quote(word)+
				"; an address must not reach a notification, in any case (Docs/01 §4.4)")
		}

		if !startsUpper(word) || allowedCapitals[word] || startsSentence(stripped, span[0]) {
			continue
		}
		problems = append(problems, "it contains the capitalised word "+strconv.Quote(word)+
			" mid-sentence, which in this copy is a person, a place or a business; only "+
			"`Shipper` and `Job` may be capitalised where a sentence has not just begun")
	}
	return problems
}

// startsUpper reports whether a word begins with an upper-case letter.
func startsUpper(word string) bool {
	r, _ := utf8.DecodeRuneInString(word)
	return unicode.IsUpper(r)
}

// startsSentence reports whether the word beginning at byte offset i is the first of a sentence.
//
// It scans backwards, rune by rune, over whitespace and over the punctuation that can sit between a
// full stop and the next word — brackets, quotation marks and the dashes the email sign-off uses —
// and answers yes at the start of the text or at terminal punctuation.
//
// **The permissive direction is deliberate.** A word this wrongly calls sentence-initial is one the
// capitalisation rule lets through, and that is the right way to be wrong: a guard that fires on
// ordinary copy gets widened until it fires on nothing, which is the failure mode of every
// heuristic anybody has ever left in a build.
func startsSentence(text string, i int) bool {
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:i])
		i -= size
		switch {
		case unicode.IsSpace(r):
		case r == '(', r == '[', r == '"', r == '\'', r == '“', r == '‘', r == '*':
		case r == '—', r == '–', r == '-':
		case r == '.', r == '!', r == '?', r == ':', r == ';':
			return true
		default:
			return false
		}
	}
	return true
}

// quoteAround is a short window of text either side of a byte offset, for a message somebody has to
// act on. A whole rendered email body inside an error is a message nobody reads.
//
// The window is cut on bytes and repaired to valid UTF-8 afterwards rather than counted in runes:
// what it is for is orientation, and a replacement character at one end costs nothing.
func quoteAround(text string, i int) string {
	const window = 12
	from := max(0, i-window)
	to := min(len(text), i+window)
	return strconv.Quote(strings.TrimSpace(strings.ToValidUTF8(text[from:to], "")))
}
