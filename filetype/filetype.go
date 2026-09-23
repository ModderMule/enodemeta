// Package filetype maps a filename to the eD2K file-type string that
// FT_FILETYPE carries.
//
// The strings are eMule's own (ED2KFTSTR_*), because that is what a client's
// type filter compares against: a search for videos matches rows whose type is
// exactly "Video". Anything this package does not recognise gets no type at
// all, which is better than a wrong one — an unknown type is invisible to a
// filtered search, while a wrong one is actively misleading.
package filetype

import (
	"strings"
)

// The eD2K type strings.
const (
	Audio      = "Audio"
	Video      = "Video"
	Image      = "Image"
	Document   = "Doc"
	Program    = "Pro"
	Archive    = "Arc"
	CDImage    = "Iso"
	Collection = "Emulecollection"
)

// FromName returns the eD2K file type for a filename, or "" when the extension
// is unknown.
func FromName(name string) string {
	ext := Extension(name)
	if ext == "" {
		return ""
	}

	// The deliberate non-entries come first, so a later edit to byExtension
	// cannot quietly overrule them.
	if neverTyped[ext] {
		return ""
	}

	if t, ok := byExtension[ext]; ok {
		return t
	}

	// RAR's old volume scheme: .r00 through .r99 follow the .rar part.
	if len(ext) == 3 && ext[0] == 'r' && isDigit(ext[1]) && isDigit(ext[2]) {
		return Archive
	}

	// 7-Zip and modern RAR volumes: .001, .002, ...
	if len(ext) == 3 && isDigit(ext[0]) && isDigit(ext[1]) && isDigit(ext[2]) {
		return Archive
	}

	return ""
}

// Extension returns a filename's lowercased extension without the dot.
func Extension(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	// Only the last path element matters, and a directory separator inside a
	// torrent path is always "/" whatever the host runs.
	if idx := strings.LastIndexByte(name, '/'); idx >= 0 {
		name = name[idx+1:]
	}

	// A leading dot names a hidden file, it does not introduce an extension:
	// path.Ext(".gitignore") is ".gitignore", and typing a file called ".zip"
	// as an archive would be worse than typing it as nothing.
	dot := strings.LastIndexByte(name, '.')
	if dot <= 0 || dot == len(name)-1 {
		return ""
	}

	return strings.ToLower(name[dot+1:])
}

// -- internals ---------------------------------------------------------------

// neverTyped are the extensions that deliberately get no type at all.
//
// All three are transport artefacts of a Usenet posting rather than content
// somebody searched for, and typing them would make a filtered search worse.
// ".nfo" is the one that has to be said out loud: it is a text file, so the
// obvious entry is Doc — and then every Doc-filtered search comes back full of
// release notes instead of documents, on a catalogue where nearly every release
// ships one. ".par2" and ".sfv" are repair and checksum data, and a release's
// recovery volumes outnumber its files.
//
// A row typed "" is invisible to a filtered search and still visible to an
// unfiltered one, which is the right outcome for all three.
var neverTyped = map[string]bool{
	"nfo":  true,
	"par2": true,
	"sfv":  true,
}

// byExtension is eMule's table, extended with the formats that appeared after
// it stopped being updated: a crawl is full of .mkv, .webm, .opus and .zst, and
// a release typed "" is one a filtered search never shows.
var byExtension = map[string]string{
	// Audio
	"aac": Audio, "ac3": Audio, "aif": Audio, "aiff": Audio, "alac": Audio,
	"amr": Audio, "ape": Audio, "au": Audio, "dts": Audio, "flac": Audio,
	"m1a": Audio, "m2a": Audio, "m4a": Audio, "mid": Audio, "midi": Audio,
	"mka": Audio, "mp1": Audio, "mp2": Audio, "mp3": Audio, "mpa": Audio,
	"mpc": Audio, "oga": Audio, "ogg": Audio, "opus": Audio, "ra": Audio,
	"shn": Audio, "wav": Audio, "wma": Audio, "wv": Audio,
	// Audiobooks and the high-resolution formats, which is what the Usenet
	// audio groups post that the torrent side rarely sees.
	"aa": Audio, "aax": Audio, "dff": Audio, "dsf": Audio, "m4b": Audio,
	"spx": Audio, "tta": Audio,

	// Video
	"3gp": Video, "asf": Video, "avi": Video, "divx": Video, "flv": Video,
	"m1v": Video, "m2ts": Video, "m2v": Video, "m4v": Video, "mkv": Video,
	"mov": Video, "mp4": Video, "mpe": Video, "mpeg": Video, "mpg": Video,
	"mpv": Video, "mts": Video, "ogm": Video, "ogv": Video, "qt": Video,
	"rm": Video, "rmvb": Video, "ts": Video, "vob": Video, "webm": Video,
	"wmv": Video, "xvid": Video, "evo": Video, "f4v": Video, "mk3d": Video,

	// Image
	"avif": Image, "bmp": Image, "gif": Image, "heic": Image, "ico": Image,
	"jfif": Image, "jpeg": Image, "jpg": Image, "pcx": Image, "png": Image,
	"psd": Image, "svg": Image, "tga": Image, "tif": Image, "tiff": Image,
	"webp": Image, "xcf": Image,

	// Documents
	"azw3": Document, "cbr": Document, "cbz": Document, "chm": Document,
	"csv": Document, "djvu": Document, "doc": Document, "docx": Document,
	"epub": Document, "fb2": Document, "htm": Document, "html": Document,
	"md": Document, "mobi": Document, "nfo": Document, "odp": Document,
	"ods": Document, "odt": Document, "pdf": Document, "ppt": Document,
	"pptx": Document, "ps": Document, "rtf": Document, "srt": Document,
	"sub": Document, "txt": Document, "xls": Document, "xlsx": Document,
	// Subtitles and comics, which a Usenet catalogue is full of and a torrent
	// one is not. A subtitle is a document: it is what a user opens next to the
	// video, and eMule's own table has "srt" and "sub" already.
	"ass": Document, "azw": Document, "cb7": Document, "cbt": Document,
	"idx": Document, "lrf": Document, "ssa": Document, "sup": Document,
	"vtt": Document,

	// Programs
	"apk": Program, "app": Program, "appimage": Program, "bat": Program,
	"com": Program, "deb": Program, "dll": Program, "exe": Program,
	"ipa": Program, "jar": Program, "msi": Program, "pkg": Program,
	"rpm": Program, "sh": Program, "so": Program, "flatpak": Program,
	"msix": Program, "run": Program, "snap": Program,

	// Archives
	"7z": Archive, "ace": Archive, "arj": Archive, "br": Archive,
	"bz2": Archive, "cab": Archive, "gz": Archive, "lha": Archive,
	"lz": Archive, "lzh": Archive, "lzma": Archive, "rar": Archive,
	"tar": Archive, "taz": Archive, "tbz": Archive, "tgz": Archive,
	"txz": Archive, "xz": Archive, "z": Archive, "zip": Archive,
	"zipx": Archive, "zst": Archive, "lz4": Archive, "sit": Archive,
	"sitx": Archive, "zpaq": Archive,

	// CD/DVD images
	"bin": CDImage, "ccd": CDImage, "cdi": CDImage, "cue": CDImage,
	"dmg": CDImage, "img": CDImage, "iso": CDImage, "mdf": CDImage,
	"mds": CDImage, "nrg": CDImage, "toast": CDImage, "vcd": CDImage,

	// eMule's own collection format
	"emulecollection": Collection,
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
