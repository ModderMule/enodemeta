package searchform

import (
	"net/url"
	"reflect"
	"testing"

	"github.com/ModderMule/enodemeta/metahash"
	"github.com/ModderMule/enodemeta/model"
)

func TestParse(t *testing.T) {
	raw := "q=+ubuntu+iso+&exclude=cam++ts&sort=SIZE&asc=1&type=Iso&kind=v1,bogus&kind=v2" +
		"&minsize=700&maxsize=4.5&maxage=30&minfiles=1&maxfiles=1" +
		"&cat=5000,5040&cat=x&cat=0&group=alt.binaries.a,+alt.binaries.b&completion=99.9&grabs=3&indexed=7" +
		"&seeders=2&leechers=1&popularity=100&alive=1&seen=14"

	values, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	got := Parse(values)
	t.Logf("input:  %s", raw)
	t.Logf("output: %+v", got)

	want := model.SearchQuery{
		Query:             "ubuntu iso",
		Exclude:           []string{"cam", "ts"},
		Sort:              model.SortSize,
		Ascending:         true,
		Type:              "Iso",
		Kinds:             []metahash.Kind{metahash.KindBTV1, metahash.KindBTV2},
		MinSize:           700 * MiB,
		MaxSize:           uint64(4.5 * MiB),
		MaxAgeDays:        30,
		MinFiles:          1,
		MaxFiles:          1,
		Categories:        []uint16{5000, 5040},
		Groups:            []string{"alt.binaries.a", "alt.binaries.b"},
		MinCompletion:     9990,
		MinGrabs:          3,
		IndexedWithinDays: 7,
		MinSeeders:        2,
		MinLeechers:       1,
		MinPopularity:     100,
		Alive:             true,
		SeenWithinDays:    14,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestParseDropsWhatDoesNotParse(t *testing.T) {
	raw := "q=x&sort=nonsense&minsize=-5&maxsize=abc&maxage=-1&completion=250&seeders=1e9&cat=99999"

	values, _ := url.ParseQuery(raw)
	got := Parse(values)
	t.Logf("input:  %s", raw)
	t.Logf("output: %+v", got)

	want := model.SearchQuery{Query: "x", MinCompletion: 10000}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestAscendingOnlyForAttributeSorts(t *testing.T) {
	for _, raw := range []string{"q=x&asc=1", "q=x&sort=relevance&asc=1", "q=x&sort=best&asc=1"} {
		values, _ := url.ParseQuery(raw)
		got := Parse(values)
		t.Logf("input: %s output: ascending=%t", raw, got.Ascending)

		if got.Ascending {
			t.Errorf("%s: a ranked sort has no direction", raw)
		}
	}
}

func TestEncodeRoundTrips(t *testing.T) {
	queries := []model.SearchQuery{
		{Query: "only keywords"},
		{
			Query:             "everything",
			Exclude:           []string{"cam", "ts"},
			Sort:              model.SortGrabs,
			Ascending:         true,
			Type:              "Video",
			Kinds:             []metahash.Kind{metahash.KindBTV2},
			MinSize:           700 * MiB,
			MaxSize:           MiB / 2,
			MaxAgeDays:        30,
			MinFiles:          2,
			MaxFiles:          9,
			Categories:        []uint16{2000},
			Groups:            []string{"alt.binaries.a"},
			MinCompletion:     9995,
			MinGrabs:          1,
			IndexedWithinDays: 3,
			MinSeeders:        5,
			MinLeechers:       6,
			MinPopularity:     7,
			Alive:             true,
			SeenWithinDays:    8,
		},
	}

	for _, q := range queries {
		encoded := Encode(q)
		back := Parse(encoded)
		t.Logf("input:  %+v", q)
		t.Logf("output: %s", encoded.Encode())

		if !reflect.DeepEqual(q, back) {
			t.Errorf("round trip lost something:\n in: %+v\nout: %+v", q, back)
		}
	}
}

func TestFiltered(t *testing.T) {
	cases := []struct {
		query model.SearchQuery
		want  bool
	}{
		{model.SearchQuery{Query: "x"}, false},
		{model.SearchQuery{Query: "x", Sort: model.SortDate}, true},
		{model.SearchQuery{Query: "x", Alive: true}, true},
		{model.SearchQuery{Query: "x", Exclude: []string{"cam"}}, true},
	}

	for _, tc := range cases {
		got := Filtered(tc.query)
		t.Logf("input: %+v output: %t", tc.query, got)
		if got != tc.want {
			t.Errorf("Filtered(%+v) = %t, want %t", tc.query, got, tc.want)
		}
	}
}

func TestFormatters(t *testing.T) {
	cases := []struct{ got, want string }{
		{FormatSize(0), ""},
		{FormatSize(700 * MiB), "700"},
		{FormatSize(MiB / 2), "0.5"},
		{FormatPercent(0), ""},
		{FormatPercent(9990), "99.9"},
		{FormatPercent(10000), "100"},
		{FormatCount(0), ""},
		{FormatCount(42), "42"},
	}

	for _, c := range cases {
		t.Logf("output: %q want %q", c.got, c.want)
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}
