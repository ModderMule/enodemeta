package nzbmeta

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxSubjectRunes is the longest subject the heuristics look at; a longer one
// yields nothing.
//
// It is eMuleQt's kMaxSubjectChars, and it is here for parity rather than for
// safety: RE2 has no catastrophic-backtracking failure mode, so Go needs no such
// bound, but a subject the two implementations read differently would give a
// release a different filename and a different completion figure on each side.
// It skips rather than truncates for the reason eMuleQt states: the part counter
// sits at the end of the line, so truncating would corrupt exactly the field the
// cap protects.
const MaxSubjectRunes = 2048

// SubjectInfo is what the heuristics recovered from a subject. Every field is
// allowed to be zero: "no counter" and "no filename" are normal outcomes.
type SubjectInfo struct {
	// FileName is empty when the subject carries no usable name.
	FileName string

	// Part and Total come from the yEnc "(n/m)" article counter, and are zero
	// when there is none.
	Part, Total int

	// FileIndex and FileTotal come from a leading "[n/m]" file counter.
	FileIndex, FileTotal int
}

// ParseSubject reads a yEnc subject line.
//
// A subject looks like one of these, and there is no standard:
//
//	[1/8] - "Some.Release.r00" yEnc (03/97)
//	Some.Release.part02.rar (12/97)
//	abcdef0123456789 [04/25] - "abcdef0123456789.vol00+01.par2" yEnc (1/9)
//	[3/9] xw8Ks92m1 - (2/181)
//
// The rules are eMuleQt's built-in set, which is in turn nZEDb's
// collection_regexes distilled, and two of them are load-bearing:
//
//  1. The "(n/m)" part counter has no end anchor — subjects carry trailing junk
//     after it, and anchoring drops a large minority of posts — and the *last*
//     parenthesised pair on the line is the counter, which is what keeps a
//     "(2011)" in a title from beating a genuine "(1/9)" at the end.
//  2. Returning nothing is allowed. Do not tighten these into something that
//     "validates" a subject; the caller is required to cope.
//
// It never fails, and it is never the authority on a filename: the real one
// arrives in the first article's "=ybegin name=", or, for a fully obfuscated
// post, in the PAR2 set.
func ParseSubject(subject string) SubjectInfo {
	var info SubjectInfo

	if subject == "" || utf8.RuneCountInString(subject) > MaxSubjectRunes {
		return info
	}

	if m, ok := firstMatching(partRules, subject); ok {
		info.Part = atoi(m.rule.group(m.match, "index"))
		info.Total = atoi(m.rule.group(m.match, "total"))
	}

	if m, ok := firstMatching(fileRules, subject); ok {
		info.FileIndex = atoi(m.rule.group(m.match, "index"))
		info.FileTotal = atoi(m.rule.group(m.match, "total"))
	}

	// The first rule that *matches* ends the role, even when what it captured
	// trims to nothing: a quoted name wins outright, so
	//
	//	Some.Release.rar " " yEnc (1/2)
	//
	// yields no filename rather than falling through to the bare-name rule.
	// Preserved from eMuleQt deliberately; changing it is its own decision.
	if m, ok := firstMatching(nameRules, subject); ok {
		info.FileName = strings.TrimSpace(m.rule.group(m.match, "name"))
	}

	return info
}

// -- internals ---------------------------------------------------------------

// subjectRule is one compiled heuristic. The named groups are resolved once,
// here, rather than per subject: eMuleQt refuses positional groups for the same
// reason, because shifting group 1 by one is all it takes to report "part 97 of
// 3" with no error anywhere.
type subjectRule struct {
	name string
	re   *regexp.Regexp

	// last takes the last match in the subject rather than the first.
	last bool
}

// group returns the named capture from a submatch slice, or "".
func (r subjectRule) group(match []string, name string) string {
	idx := r.re.SubexpIndex(name)
	if idx < 0 || idx >= len(match) {
		return ""
	}

	return match[idx]
}

type subjectHit struct {
	rule  subjectRule
	match []string
}

// The built-in rule sets, in the order they are tried. They are the patterns
// printed in eMuleQt's docs/usenet-module.md, spelled with Go's (?P<>) syntax
// and with the case-insensitive flag inline.
var (
	partRules = []subjectRule{{
		name: "yenc-part-counter",
		re:   regexp.MustCompile(`(?i)\((?P<index>\d{1,5})\s*/\s*(?P<total>\d{1,5})\)`),
		last: true,
	}}

	fileRules = []subjectRule{{
		name: "bracketed-file-counter",
		re:   regexp.MustCompile(`(?i)\[(?P<index>\d{1,5})\s*/\s*(?P<total>\d{1,5})\]`),
	}}

	nameRules = []subjectRule{
		{
			// A quoted filename wins outright: posters who quote mean it.
			name: "quoted",
			re:   regexp.MustCompile(`"(?P<name>[^"]{1,255})"`),
		},
		{
			// An unquoted token that looks like a filename: a dot followed by a
			// plausible extension. Deliberately narrow — anything looser starts
			// matching release names with dots in them.
			name: "bare-extension",
			re:   regexp.MustCompile(`(?i)(?P<name>[^\s"]+\.(?:part\d+\.rar|vol\d+\+\d+\.par2|par2|rar|r\d{2,3}|7z|zip|nfo|sfv|mkv|mp4|avi|iso|\d{3}))`),
			last: true,
		},
	}
)

// firstMatching returns the first rule of the set that matches, with the match
// its pick asked for.
func firstMatching(rules []subjectRule, subject string) (subjectHit, bool) {
	for _, rule := range rules {
		if rule.last {
			all := rule.re.FindAllStringSubmatch(subject, -1)
			if len(all) == 0 {
				continue
			}

			return subjectHit{rule: rule, match: all[len(all)-1]}, true
		}

		if match := rule.re.FindStringSubmatch(subject); match != nil {
			return subjectHit{rule: rule, match: match}, true
		}
	}

	return subjectHit{}, false
}

func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}

	return n
}
