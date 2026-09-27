package filetype

import "testing"

func TestFromName(t *testing.T) {
	cases := []struct {
		label string
		name  string
		want  string
	}{
		{"a video", "Some.Release.2026.1080p.mkv", Video},
		{"an older video container", "movie.avi", Video},
		{"audio", "track01.flac", Audio},
		{"modern audio", "podcast.opus", Audio},
		{"an image", "cover.JPG", Image},
		{"a document", "readme.txt", Document},
		{"a release note gets no type on purpose", "release.nfo", ""},
		{"par2 recovery data gets no type on purpose", "release.vol000+01.par2", ""},
		{"a checksum file gets no type on purpose", "release.sfv", ""},
		{"an audiobook", "book.m4b", Audio},
		{"a subtitle track", "movie.ass", Document},
		{"a program", "setup.exe", Program},
		{"an archive", "release.7z", Archive},
		{"a modern compressor", "backup.tar.zst", Archive},
		{"a disc image", "ubuntu-24.04.iso", CDImage},
		{"an eMule collection", "pack.emulecollection", Collection},
		{"an old RAR volume", "release.r00", Archive},
		{"a late RAR volume", "release.r42", Archive},
		{"a numbered volume", "release.001", Archive},
		{"a path, not just a name", "Extras/Sample/clip.mp4", Video},
		{"an unknown extension gets no type at all", "data.qqq", ""},
		{"no extension", "README", ""},
		{"an empty name", "", ""},
		{"a dotfile with no extension", ".gitignore", ""},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %q", c.name)

			got := FromName(c.name)
			t.Logf("output: %q", got)

			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestExtension(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"a.MKV", "mkv"},
		{"  spaced.mp3  ", "mp3"},
		{"dir.with.dots/file.txt", "txt"},
		{"noext", ""},
		{"trailing.", ""},
	}

	for _, c := range cases {
		t.Logf("input:  %q", c.in)

		got := Extension(c.in)
		t.Logf("output: %q", got)

		if got != c.want {
			t.Errorf("%q: got %q, want %q", c.in, got, c.want)
		}
	}
}

// TestDotfileIsNotAnExtension covers the case that would otherwise type every
// hidden file as whatever its name looks like: ".gitignore" has no extension,
// it has a name that starts with a dot.
func TestDotfileIsNotAnExtension(t *testing.T) {
	for _, name := range []string{".gitignore", ".env", "dir/.hidden"} {
		t.Logf("input:  %q", name)

		if got := Extension(name); got != "" {
			t.Errorf("%q must have no extension, got %q", name, got)
		}
	}
	t.Logf("output: none of them has an extension")
}

func TestDominant(t *testing.T) {
	const mb = 1 << 20

	cases := []struct {
		label string
		files []Weighted
		want  string
	}{
		{"a video pack with subtitles and a release note", []Weighted{
			{"Show.S01/Show.S01E01.mkv", 1200 * mb},
			{"Show.S01/Show.S01E02.mkv", 1100 * mb},
			{"Show.S01/Show.S01E01.srt", 1 * mb},
			{"Show.S01/Show.S01E02.srt", 1 * mb},
			{"Show.S01/Show.S01.nfo", 1 * mb},
		}, Video},
		{"an album with its cover art", []Weighted{
			{"01 - Intro.flac", 30 * mb},
			{"02 - Song.flac", 40 * mb},
			{"cover.jpg", 5 * mb},
		}, Audio},
		{"a disc image with a readme", []Weighted{
			{"readme.txt", 1 * mb},
			{"distro.iso", 4000 * mb},
		}, CDImage},
		{"bytes beat file count", []Weighted{
			{"a.srt", 1 * mb}, {"b.srt", 1 * mb}, {"c.srt", 1 * mb},
			{"movie.mp4", 700 * mb},
		}, Video},
		{"a rar posting is an archive", []Weighted{
			{"release.part01.rar", 500 * mb},
			{"release.part02.rar", 500 * mb},
			{"release.vol00+01.par2", 50 * mb},
		}, Archive},
		{"nothing typed", []Weighted{
			{"release.nfo", 1 * mb},
			{"release.par2", 1 * mb},
			{"data.qqq", 900 * mb},
		}, ""},
		{"a tie goes to the type seen first", []Weighted{
			{"clip.mp4", 100 * mb},
			{"song.mp3", 100 * mb},
		}, Video},
		{"no files", nil, ""},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			t.Logf("input:  %+v", c.files)

			got := Dominant(c.files)
			t.Logf("output: %q", got)

			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestAllIsEveryTypeOnce(t *testing.T) {
	all := All()
	t.Logf("input:  All()")
	t.Logf("output: %v", all)

	seen := map[string]bool{}
	for _, name := range all {
		if name == "" || seen[name] {
			t.Errorf("type %q is empty or listed twice", name)
		}
		seen[name] = true
	}

	for _, name := range []string{Audio, Video, Image, Document, Program, Archive, CDImage, Collection} {
		if !seen[name] {
			t.Errorf("type %q is missing", name)
		}
	}

	all[0] = "changed"
	if All()[0] != Audio {
		t.Errorf("All must return a fresh slice")
	}
}

func TestLabel(t *testing.T) {
	cases := map[string]string{
		Audio:      "Audio",
		Document:   "Document",
		CDImage:    "CD image",
		Collection: "Collection",
		"Other":    "Other",
	}

	for in, want := range cases {
		got := Label(in)
		t.Logf("input: %q output: %q", in, got)
		if got != want {
			t.Errorf("Label(%q) = %q, want %q", in, got, want)
		}
	}
}
