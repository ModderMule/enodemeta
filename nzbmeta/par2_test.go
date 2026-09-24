package nzbmeta

import "testing"

func TestPAR2Names(t *testing.T) {
	for _, c := range []struct {
		label      string
		name       string
		wantPAR2   bool
		wantBlocks int
	}{
		{"the index file holds no recovery data", "release.par2", true, 0},
		{"par2cmdline's unpadded volume", "release.vol0+1.par2", true, 1},
		{"QuickPar's padded volume", "release.vol000+01.par2", true, 1},
		{"MultiPar's wider padding", "release.vol0012+0034.par2", true, 34},
		{"case is not significant", "RELEASE.VOL00+04.PAR2", true, 4},
		{"a volume inside a path", "Some.Release/rel.vol07+08.par2", true, 8},
		{"an archive volume is not par2", "release.part01.rar", false, 0},
		{"a name that merely mentions par2", "how.to.par2.txt", true, 0},
		{"nothing", "", false, 0},
	} {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %q", c.name)

			gotPAR2, gotBlocks := IsPAR2(c.name), RecoveryBlocks(c.name)
			t.Logf("output: isPar2=%v blocks=%d", gotPAR2, gotBlocks)

			if gotPAR2 != c.wantPAR2 || gotBlocks != c.wantBlocks {
				t.Errorf("got %v/%d, want %v/%d", gotPAR2, gotBlocks, c.wantPAR2, c.wantBlocks)
			}
		})
	}
}

// TestFilePAR2FallsBackToTheSubject is the rule that makes this work on
// obfuscated posts: the filename is scrambled but the subject usually still
// carries the token.
func TestFilePAR2FallsBackToTheSubject(t *testing.T) {
	for _, c := range []struct {
		label      string
		file       File
		wantPAR2   bool
		wantBlocks int
	}{
		{
			label:    "the filename decides when there is one",
			file:     File{FileName: "rel.vol00+03.par2", Subject: "nothing useful"},
			wantPAR2: true, wantBlocks: 3,
		},
		{
			label:    "the subject decides when there is not",
			file:     File{Subject: `[2/9] - "rel.vol00+05.par2" yEnc (1/1)`},
			wantPAR2: true, wantBlocks: 5,
		},
		{
			label:    "a filename that is not par2 is not overruled by the subject",
			file:     File{FileName: "rel.r01", Subject: "rel.vol00+05.par2"},
			wantPAR2: false, wantBlocks: 0,
		},
	} {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  name=%q subject=%q", c.file.FileName, c.file.Subject)

			gotPAR2, gotBlocks := c.file.IsPAR2(), c.file.RecoveryBlocks()
			t.Logf("output: isPar2=%v isVolume=%v blocks=%d", gotPAR2, c.file.IsPAR2Volume(), gotBlocks)

			if gotPAR2 != c.wantPAR2 || gotBlocks != c.wantBlocks {
				t.Errorf("got %v/%d, want %v/%d", gotPAR2, gotBlocks, c.wantPAR2, c.wantBlocks)
			}
		})
	}
}
