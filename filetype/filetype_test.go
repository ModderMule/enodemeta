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
		{"a release note, which is a document", "release.nfo", Document},
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
