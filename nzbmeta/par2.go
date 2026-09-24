package nzbmeta

import (
	"regexp"
	"strconv"
	"strings"
)

// par2VolumeRe reads a PAR2 recovery volume's block count out of its name.
//
// Both spellings occur — par2cmdline writes "rel.vol0+1.par2", QuickPar and
// MultiPar pad to "rel.vol000+01.par2" — so the digits are parsed rather than
// matched.
var par2VolumeRe = regexp.MustCompile(`(?i)\.vol(\d+)\+(\d+)\.par2`)

// IsPAR2 reports whether a name belongs to a PAR2 set: the index file or a
// recovery volume.
func IsPAR2(name string) bool {
	return strings.Contains(strings.ToLower(name), ".par2")
}

// RecoveryBlocks is how many recovery blocks a PAR2 volume carries, read out of
// a ".vol{start}+{count}.par2" name.
//
// Zero for the index ".par2", which holds the file list and no recovery data at
// all, and zero for everything that is not PAR2.
//
// This is what makes on-demand repair possible for a client: it can total up
// exactly enough volumes to cover the damage PAR2 reported, instead of fetching a
// recovery set it will usually throw away.
func RecoveryBlocks(name string) int {
	m := par2VolumeRe.FindStringSubmatch(name)
	if m == nil {
		return 0
	}

	// The second number is the block count; the first is the starting exponent
	// and says nothing about how much recovery data is here.
	blocks, err := strconv.Atoi(m[2])
	if err != nil {
		return 0
	}

	return blocks
}

// IsPAR2 reports whether this file belongs to a PAR2 set.
//
// The filename is not always available — an obfuscated post scrambles it — so it
// falls back to the subject, which usually still carries the ".par2" token even
// when the name itself has been mangled.
func (f File) IsPAR2() bool { return IsPAR2(f.par2Haystack()) }

// RecoveryBlocks is how many PAR2 recovery blocks this file carries. Zero for
// the index file and for everything that is not PAR2.
func (f File) RecoveryBlocks() int { return RecoveryBlocks(f.par2Haystack()) }

// IsPAR2Volume is true for a recovery volume as opposed to the index file. These
// are the files an initial download plan leaves out.
func (f File) IsPAR2Volume() bool { return f.RecoveryBlocks() > 0 }

// -- internals ---------------------------------------------------------------

func (f File) par2Haystack() string {
	if f.FileName != "" {
		return f.FileName
	}

	return f.Subject
}
