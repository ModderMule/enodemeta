package model

import "testing"

// TestWorkFilterIsZero: the zero filter narrows nothing, and each kind of field set makes it
// narrow something.
func TestWorkFilterIsZero(t *testing.T) {
	for _, c := range []struct {
		name   string
		filter WorkFilter
		zero   bool
	}{
		{"zero", WorkFilter{}, true},
		{"a kind", WorkFilter{Kinds: []WorkKind{WorkMovie}}, false},
		{"an id", WorkFilter{TVDBID: 81189}, false},
		{"a UPC", WorkFilter{UPC: "618582602743"}, false},
		{"a score floor", WorkFilter{MinIMDbScore: 70}, false},
		{"a runtime ceiling", WorkFilter{MaxRuntime: 45}, false},
		{"a performer", WorkFilter{Performers: []string{"Chennin Blanc"}}, false},
		{"linked only", WorkFilter{LinkedOnly: true}, false},
		{"an artist", WorkFilter{Creator: "Daft Punk"}, false},
		{"an AniDB id", WorkFilter{AniDBID: 14758}, false},
		{"an ISBN", WorkFilter{ISBN: "9780547928227"}, false},
	} {
		t.Logf("input:  %s %+v", c.name, c.filter)
		t.Logf("output: zero %v, ids %v", c.filter.IsZero(), c.filter.HasIDs())

		if c.filter.IsZero() != c.zero {
			t.Errorf("%s: IsZero = %v, want %v", c.name, c.filter.IsZero(), c.zero)
		}
	}
}

// TestWorkKindValid: the seven kinds are valid, unspecified and past the last are not.
func TestWorkKindValid(t *testing.T) {
	for kind := WorkKind(0); kind <= workKindCount; kind++ {
		want := kind >= WorkMovie && kind <= WorkBook

		t.Logf("input:  %d; output: %v", kind, kind.Valid())

		if kind.Valid() != want {
			t.Errorf("%d: Valid = %v, want %v", kind, kind.Valid(), want)
		}
	}
}
