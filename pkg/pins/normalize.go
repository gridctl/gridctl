package pins

// Text normalization for the tool-poisoning scanner. Injection payloads evade
// naive pattern matching with zero-width characters, cross-script homoglyphs,
// combining-mark noise, and leetspeak; patterns therefore match against the
// output of normalizeToolText, never raw text. Invisible-character DETECTION
// (P005) runs on the raw text separately, because the presence of hidden
// characters is itself a finding.
//
// The invisible-range table, confusable map, and pipeline composition are
// derived from Pipelock (github.com/luckyPipewrench/pipelock),
// internal/normalize/normalize.go, Copyright 2026 Josh Waldrep,
// licensed under the Apache License 2.0.

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// invisibleRanges enumerates characters that render invisibly (or not at all)
// in most UIs but survive as tokens in LLM tokenizers: the classic vehicle for
// instructions a human reviewer cannot see. This table is the single source of
// truth for detection; it mirrors the enumerated ranges in the web UI's
// nonPrintable.ts so both surfaces agree on what counts as hidden.
var invisibleRanges = &unicode.RangeTable{
	R16: []unicode.Range16{
		{Lo: 0x00AD, Hi: 0x00AD, Stride: 1}, // soft hyphen
		{Lo: 0x115F, Hi: 0x1160, Stride: 1}, // Hangul fillers
		{Lo: 0x200B, Hi: 0x200F, Stride: 1}, // zero-width space through RTL mark
		{Lo: 0x202A, Hi: 0x202E, Stride: 1}, // bidi embedding controls
		{Lo: 0x2060, Hi: 0x2064, Stride: 1}, // word joiner through invisible plus
		{Lo: 0x2066, Hi: 0x2069, Stride: 1}, // bidi isolate controls
		{Lo: 0x3164, Hi: 0x3164, Stride: 1}, // Hangul filler
		{Lo: 0xFE00, Hi: 0xFE0F, Stride: 1}, // variation selectors 1-16
		{Lo: 0xFEFF, Hi: 0xFEFF, Stride: 1}, // BOM / ZWNBSP
		{Lo: 0xFFF9, Hi: 0xFFFB, Stride: 1}, // interlinear annotation anchors
	},
	R32: []unicode.Range32{
		{Lo: 0xE0000, Hi: 0xE007F, Stride: 1}, // Tags block (ASCII smuggling)
		{Lo: 0xE0100, Hi: 0xE01EF, Stride: 1}, // variation selectors supplement
	},
}

// confusableMap folds cross-script homoglyphs to Latin. NFKC does not handle
// these: Cyrillic а (U+0430) stays Cyrillic under NFKC. Covers the scripts and
// symbol blocks that read as Latin to an LLM (Cyrillic, Greek, Latin stroke
// letters, IPA small caps, negative-squared letters, regional indicators);
// deliberately not exhaustive beyond characters plausible in English-language
// injection phrases.
var confusableMap = map[rune]rune{
	// Cyrillic uppercase
	'А': 'A', 'В': 'B', 'С': 'C', 'Е': 'E', 'Н': 'H',
	'І': 'I', 'Ј': 'J', 'К': 'K', 'М': 'M', 'О': 'O',
	'Р': 'P', 'Ѕ': 'S', 'Т': 'T', 'Х': 'X',
	// Cyrillic lowercase
	'а': 'a', 'в': 'v', 'е': 'e', 'н': 'h', 'і': 'i',
	'к': 'k', 'м': 'm', 'о': 'o', 'р': 'p', 'с': 'c',
	'т': 't', 'у': 'y', 'х': 'x', 'ј': 'j', 'ѕ': 's',
	// Greek uppercase
	'Α': 'A', 'Β': 'B', 'Ε': 'E', 'Ζ': 'Z', 'Η': 'H',
	'Ι': 'I', 'Κ': 'K', 'Μ': 'M', 'Ν': 'N', 'Ο': 'O',
	'Ρ': 'P', 'Τ': 'T', 'Υ': 'Y', 'Χ': 'X',
	// Greek lowercase
	'α': 'a', 'ε': 'e', 'ι': 'i', 'κ': 'k', 'ν': 'v',
	'ο': 'o',
	// Latin stroke/bar letters (the stroke is integral; NFD does not decompose)
	'Ø': 'O', 'ø': 'o', 'Đ': 'D', 'đ': 'd', 'Ł': 'L',
	'ł': 'l', 'Ħ': 'H', 'ħ': 'h', 'Ŧ': 'T', 'ŧ': 't',
	// Latin Extended / IPA small caps (survive NFKC)
	'ᴀ': 'A', 'ʙ': 'B', 'ᴄ': 'C', 'ᴅ': 'D', 'ᴇ': 'E',
	'ꜰ': 'F', 'ɢ': 'G', 'ʜ': 'H', 'ɪ': 'I', 'ᴊ': 'J',
	'ᴋ': 'K', 'ʟ': 'L', 'ᴍ': 'M', 'ɴ': 'N', 'ᴏ': 'O',
	'ᴘ': 'P', 'ʀ': 'R', 'ꜱ': 'S', 'ᴛ': 'T', 'ᴜ': 'U',
	'ᴠ': 'V', 'ᴡ': 'W', 'ʏ': 'Y', 'ᴢ': 'Z',
	// Negative Squared Latin Capital Letters (emoji-style boxed letters)
	'\U0001F170': 'A', '\U0001F171': 'B', '\U0001F172': 'C', '\U0001F173': 'D',
	'\U0001F174': 'E', '\U0001F175': 'F', '\U0001F176': 'G', '\U0001F177': 'H',
	'\U0001F178': 'I', '\U0001F179': 'J', '\U0001F17A': 'K', '\U0001F17B': 'L',
	'\U0001F17C': 'M', '\U0001F17D': 'N', '\U0001F17E': 'O', '\U0001F17F': 'P',
	'\U0001F180': 'Q', '\U0001F181': 'R', '\U0001F182': 'S', '\U0001F183': 'T',
	'\U0001F184': 'U', '\U0001F185': 'V', '\U0001F186': 'W', '\U0001F187': 'X',
	'\U0001F188': 'Y', '\U0001F189': 'Z',
	// Regional Indicator Symbols (individually render as circled letters)
	'\U0001F1E6': 'A', '\U0001F1E7': 'B', '\U0001F1E8': 'C', '\U0001F1E9': 'D',
	'\U0001F1EA': 'E', '\U0001F1EB': 'F', '\U0001F1EC': 'G', '\U0001F1ED': 'H',
	'\U0001F1EE': 'I', '\U0001F1EF': 'J', '\U0001F1F0': 'K', '\U0001F1F1': 'L',
	'\U0001F1F2': 'M', '\U0001F1F3': 'N', '\U0001F1F4': 'O', '\U0001F1F5': 'P',
	'\U0001F1F6': 'Q', '\U0001F1F7': 'R', '\U0001F1F8': 'S', '\U0001F1F9': 'T',
	'\U0001F1FA': 'U', '\U0001F1FB': 'V', '\U0001F1FC': 'W', '\U0001F1FD': 'X',
	'\U0001F1FE': 'Y', '\U0001F1FF': 'Z',
}

// leetMap folds digit-and-symbol substitutions used to evade keyword matching.
var leetMap = map[rune]rune{
	'0': 'o', '1': 'i', '3': 'e', '4': 'a', '5': 's', '7': 't', '@': 'a', '$': 's',
}

// exoticWhitespace maps non-ASCII whitespace to a plain space so word
// boundaries survive normalization ("ignore\u00A0all" matches "ignore all").
// Escaped forms only: raw invisible literals in source are the very pattern
// this package exists to flag.
var exoticWhitespace = map[rune]bool{
	'\u00A0': true, // no-break space
	'\u1680': true, // Ogham space mark
	'\u180E': true, // Mongolian vowel separator
	'\u2000': true, '\u2001': true, '\u2002': true, '\u2003': true,
	'\u2004': true, '\u2005': true, '\u2006': true, '\u2007': true,
	'\u2008': true, '\u2009': true, '\u200A': true,
	'\u2028': true, '\u2029': true, // line and paragraph separators
	'\u202F': true, // narrow no-break space
	'\u205F': true, // medium mathematical space
	'\u3000': true, // ideographic space
}

// normalizeBase runs the anti-evasion pipeline minus the leetspeak fold: fold
// controls to spaces and delete invisibles, NFKC compatibility fold,
// cross-script confusable fold, combining-mark strip, and exotic-whitespace
// fold. Unlike Pipelock's ForToolText, control characters (\t\n\r) become
// spaces rather than being deleted, so multi-line descriptions keep their word
// boundaries. Leetspeak is applied as a separate second pass (leetFold)
// because it corrupts legitimate digit-bearing patterns ("p12" becomes "pi2"),
// so pattern banks match against both forms instead of only the folded one.
func normalizeBase(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case unicode.Is(invisibleRanges, r):
			return -1
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, s)

	s = norm.NFKC.String(s)

	s = strings.Map(func(r rune) rune {
		if folded, ok := confusableMap[r]; ok {
			return folded
		}
		return r
	}, s)

	s = stripCombiningMarks(s)

	return strings.Map(func(r rune) rune {
		if exoticWhitespace[r] {
			return ' '
		}
		return r
	}, s)
}

// leetFold applies the digit-and-symbol substitutions. Every mapping is
// single-byte ASCII to single-byte ASCII, so byte offsets into a folded
// string line up with offsets into its base form (quoted-span positions
// computed on the base remain valid for matches on the fold).
func leetFold(s string) string {
	return strings.Map(func(r rune) rune {
		if folded, ok := leetMap[r]; ok {
			return folded
		}
		return r
	}, s)
}

// normalizeToolText is the full pipeline (base plus leetspeak), kept for
// callers and tests that want the most aggressive form.
func normalizeToolText(s string) string {
	return leetFold(normalizeBase(s))
}

// stripCombiningMarks decomposes to NFD and drops nonspacing marks, defeating
// accent and Zalgo noise ("ïgnorè" matches "ignore").
func stripCombiningMarks(s string) string {
	decomposed := norm.NFD.String(s)
	var b strings.Builder
	b.Grow(len(decomposed))
	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// invisibleKind names the category of a hidden character for reporting.
func invisibleKind(r rune) string {
	switch {
	case r >= 0xE0000 && r <= 0xE007F:
		return "tag-block"
	case r >= 0x200B && r <= 0x200F, r == 0x2060, r == 0xFEFF:
		return "zero-width"
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return "bidi-control"
	case r >= 0xFE00 && r <= 0xFE0F, r >= 0xE0100 && r <= 0xE01EF:
		return "variation-selector"
	case unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r':
		return "control"
	default:
		return "other-invisible"
	}
}

// invisibleReport summarizes hidden characters found in a raw string.
type invisibleReport struct {
	kinds   []string // distinct categories, insertion-ordered
	count   int      // total hidden characters
	decoded string   // ASCII message decoded from a tag-block sequence, if any
}

// detectInvisible scans raw (pre-normalization) text for hidden characters and
// decodes any Tags-block sequence back to ASCII: each tag codepoint is its
// ASCII value plus 0xE0000, so a run of tag characters is a smuggled message
// the UI never displays but the model reads.
func detectInvisible(s string) invisibleReport {
	var rep invisibleReport
	seen := make(map[string]bool)
	var decoded strings.Builder
	for _, r := range s {
		hidden := unicode.Is(invisibleRanges, r) ||
			(unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r')
		if !hidden {
			continue
		}
		rep.count++
		kind := invisibleKind(r)
		if !seen[kind] {
			seen[kind] = true
			rep.kinds = append(rep.kinds, kind)
		}
		if r >= 0xE0020 && r <= 0xE007E {
			decoded.WriteByte(byte(r - 0xE0000))
		}
	}
	rep.decoded = decoded.String()
	return rep
}
