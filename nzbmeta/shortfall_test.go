package nzbmeta

import "testing"

func TestShortfall(t *testing.T) {
	// A release that lost two of its 40 archive articles and ships one recovery
	// volume big enough to cover them.
	doc := (&Builder{}).
		AddFile(File{Subject: `"rel.part01.rar" yEnc (1/40)`, Segments: segsOf(38, 700_000)}).
		AddFile(File{Subject: `"rel.nfo" yEnc (1/1)`, Segments: segsOf(1, 2_000)}).
		AddFile(File{Subject: `"rel.vol000+20.par2" yEnc (1/3)`, Segments: segsOf(3, 700_000)}).
		Document()

	got := doc.Shortfall()
	t.Logf("input:  %d files, %d segments", len(doc.Files), doc.SegmentCount())
	t.Logf("output: listed=%d missing=%d missingBytes=%d recoveryBytes=%d unknown=%d percent=%d recoverable=%v",
		got.ListedSegments, got.MissingSegments, got.MissingBytes, got.RecoveryBytes,
		got.UnknownFiles, got.Percent(), got.LikelyRecoverable())

	if got.ListedSegments != 42 || got.MissingSegments != 2 {
		t.Errorf("listed=%d missing=%d, want 42 and 2", got.ListedSegments, got.MissingSegments)
	}

	// Priced at the archive file's own mean, not the release's: one mean over a
	// 700 KB volume and a 2 KB .nfo would misprice whichever kind lost articles.
	if got.MissingBytes != 2*700_000 {
		t.Errorf("missingBytes=%d, want %d", got.MissingBytes, 2*700_000)
	}
	if got.RecoveryBytes != 3*700_000 {
		t.Errorf("recoveryBytes=%d, want %d", got.RecoveryBytes, 3*700_000)
	}
	if got.UnknownFiles != 0 {
		t.Errorf("unknownFiles=%d, want 0", got.UnknownFiles)
	}
	if got.Percent() != 95 {
		t.Errorf("percent=%d, want 95 (42 of 44)", got.Percent())
	}
	if got.Complete() {
		t.Error("two missing articles is not complete")
	}
	if !got.LikelyRecoverable() {
		t.Error("20 recovery blocks over 2 missing articles must read as recoverable")
	}
}

// TestShortfallOfAnObfuscatedRelease pins the answer that is easy to get wrong:
// no counter means no opinion, which reports 100 and one unknown file. Reading
// that as "all missing" would make every obfuscated release look unfetchable.
func TestShortfallOfAnObfuscatedRelease(t *testing.T) {
	doc := (&Builder{}).
		AddFile(File{Subject: "xw8Ks92m1", Segments: segsOf(20, 700_000)}).
		Document()

	got := doc.Shortfall()
	t.Logf("input:  one file with no part counter and %d segments", doc.SegmentCount())
	t.Logf("output: percent=%d unknown=%d complete=%v", got.Percent(), got.UnknownFiles, got.Complete())

	if got.Percent() != 100 || got.UnknownFiles != 1 || !got.Complete() {
		t.Errorf("got percent=%d unknown=%d complete=%v, want 100/1/true",
			got.Percent(), got.UnknownFiles, got.Complete())
	}
}

func TestShortfallEdges(t *testing.T) {
	for _, c := range []struct {
		label           string
		in              Shortfall
		wantPercent     int
		wantRecoverable bool
	}{
		{"nothing at all", Shortfall{}, 100, true},
		{"everything listed", Shortfall{ListedSegments: 10}, 100, true},
		{"half listed", Shortfall{ListedSegments: 5, MissingSegments: 5}, 50, true},
		{"nothing listed", Shortfall{MissingSegments: 5, MissingBytes: 1}, 0, false},
		{"recovery exactly covers it", Shortfall{ListedSegments: 9, MissingSegments: 1, MissingBytes: 100, RecoveryBytes: 100}, 90, true},
		{"recovery one byte short", Shortfall{ListedSegments: 9, MissingSegments: 1, MissingBytes: 100, RecoveryBytes: 99}, 90, false},
		{"truncation rounds down, which is what keeps 99.97 off 100", Shortfall{ListedSegments: 9997, MissingSegments: 3}, 99, true},
	} {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %+v", c.in)
			t.Logf("output: percent=%d recoverable=%v", c.in.Percent(), c.in.LikelyRecoverable())

			if c.in.Percent() != c.wantPercent || c.in.LikelyRecoverable() != c.wantRecoverable {
				t.Errorf("got %d/%v, want %d/%v",
					c.in.Percent(), c.in.LikelyRecoverable(), c.wantPercent, c.wantRecoverable)
			}
		})
	}
}

// -- helpers -----------------------------------------------------------------

// segsOf builds n segments of the given encoded size, numbered 1..n.
func segsOf(n int, size uint64) []Segment {
	out := make([]Segment, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, Segment{MessageID: "a@h", Bytes: size, Number: i})
	}

	return out
}
