package nzbmeta

import (
	"strings"
	"testing"
)

// TestParseSubject covers the four shapes eMuleQt's header documents plus the
// ones that make the two "pick" rules load-bearing.
func TestParseSubject(t *testing.T) {
	for _, c := range []struct {
		label string
		in    string
		want  SubjectInfo
	}{
		{
			label: "a quoted name with both counters",
			in:    `[1/8] - "Some.Release.r00" yEnc (03/97)`,
			want:  SubjectInfo{FileName: "Some.Release.r00", Part: 3, Total: 97, FileIndex: 1, FileTotal: 8},
		},
		{
			label: "an unquoted name",
			in:    "Some.Release.part02.rar (12/97)",
			want:  SubjectInfo{FileName: "Some.Release.part02.rar", Part: 12, Total: 97},
		},
		{
			label: "an obfuscated post that still names its par2",
			in:    `abcdef0123456789 [04/25] - "abcdef0123456789.vol00+01.par2" yEnc (1/9)`,
			want:  SubjectInfo{FileName: "abcdef0123456789.vol00+01.par2", Part: 1, Total: 9, FileIndex: 4, FileTotal: 25},
		},
		{
			label: "no name at all, which is normal",
			in:    "[3/9] xw8Ks92m1 - (2/181)",
			want:  SubjectInfo{Part: 2, Total: 181, FileIndex: 3, FileTotal: 9},
		},
		{
			label: "a year in the title must not beat the counter at the end",
			in:    "Some.Movie.(2011).1080p.mkv (4/56)",
			want:  SubjectInfo{FileName: "Some.Movie.(2011).1080p.mkv", Part: 4, Total: 56},
		},
		{
			label: "trailing junk after the counter, which is why there is no end anchor",
			in:    `"rel.r01" yEnc (5/20) 734003200 bytes`,
			want:  SubjectInfo{FileName: "rel.r01", Part: 5, Total: 20},
		},
		{
			label: "spaces around the slash",
			in:    "rel.rar (7 / 12)",
			want:  SubjectInfo{FileName: "rel.rar", Part: 7, Total: 12},
		},
		{
			// Faithful to eMuleQt: the pattern allows whitespace around the
			// slash and not next to the parentheses. Written down because it
			// looks like an omission and is not one — both sides must read the
			// same subject the same way.
			label: "but not spaces next to the parentheses",
			in:    "rel.rar ( 7 / 12 )",
			want:  SubjectInfo{FileName: "rel.rar"},
		},
		{
			label: "an empty quoted name wins outright and yields nothing",
			in:    `Some.Release.rar " " yEnc (1/2)`,
			want:  SubjectInfo{Part: 1, Total: 2},
		},
		{
			label: "no counter at all",
			in:    "Some.Release.rar",
			want:  SubjectInfo{FileName: "Some.Release.rar"},
		},
		{
			label: "nothing recognisable",
			in:    "xw8Ks92m1",
			want:  SubjectInfo{},
		},
		{
			label: "empty",
			in:    "",
			want:  SubjectInfo{},
		},
	} {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %q", c.in)

			got := ParseSubject(c.in)
			t.Logf("output: name=%q part=%d total=%d fileIndex=%d fileTotal=%d",
				got.FileName, got.Part, got.Total, got.FileIndex, got.FileTotal)

			if got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

// TestParseSubjectSkipsAnOverlongSubject pins the eMuleQt parity bound. It skips
// rather than truncates because the part counter sits at the end of the line, so
// truncating would corrupt exactly the field the cap protects.
func TestParseSubjectSkipsAnOverlongSubject(t *testing.T) {
	long := strings.Repeat("a", MaxSubjectRunes) + ` "rel.rar" (1/2)`
	t.Logf("input:  a subject of %d runes", len([]rune(long)))

	got := ParseSubject(long)
	t.Logf("output: %+v", got)

	if got != (SubjectInfo{}) {
		t.Errorf("a subject longer than MaxSubjectRunes must yield nothing, got %+v", got)
	}

	// One rune shorter and it parses, so the bound is the bound and not an
	// accident of the pattern.
	short := strings.Repeat("a", MaxSubjectRunes-len(` "rel.rar" (1/2)`)) + ` "rel.rar" (1/2)`
	if got := ParseSubject(short); got.Total != 2 {
		t.Errorf("a subject at the bound must parse, got %+v", got)
	}
}
