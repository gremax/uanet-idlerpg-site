package main

import (
	"bytes"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the golden pages in testdata/golden")

func TestDuration(t *testing.T) {
	for seconds, want := range map[int64]string{
		0:              "0 днів, 00:00:00",
		-5:             "0 днів, 00:00:00",
		403:            "0 днів, 00:06:43",
		86400:          "1 день, 00:00:00",
		172800:         "2 дні, 00:00:00",
		950400:         "11 днів, 00:00:00",
		1814400 + 3723: "21 день, 01:02:03",
	} {
		if got := duration(seconds); got != want {
			t.Errorf("duration(%d) = %q, want %q", seconds, got, want)
		}
	}
}

func TestElapsed(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                "0 хв",
		59 * time.Minute: "59 хв",
		time.Hour:        "1 годину",
		3 * time.Hour:    "3 години",
		11 * time.Hour:   "11 годин",
		47 * time.Hour:   "47 годин",
		48 * time.Hour:   "2 дні",
		120 * time.Hour:  "5 днів",
	} {
		if got := elapsed(d); got != want {
			t.Errorf("elapsed(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestKyiv(t *testing.T) {
	summer := time.Date(2026, 7, 1, 9, 5, 0, 0, time.UTC)
	if got := kyivTime(summer); got != "12:05" {
		t.Errorf("summer time: %q, want 12:05", got)
	}
	if got := kyivDate(now); got != "21 жовтня 2026 р. о 04:47" {
		t.Errorf("kyivDate = %q", got)
	}
}

func TestLinkNames(t *testing.T) {
	players := []*Player{{Name: "ira"}, {Name: "Don"}, {Name: "Donna"}, {Name: "a<b"}, {Name: "[x]"}}
	for text, want := range map[string]string{
		"ira і Don":   `<a href="/player/ira">ira</a> і <a href="/player/Don">Don</a>`,
		"irate Donna": `irate <a href="/player/Donna">Donna</a>`,
		"Don, ira!":   `<a href="/player/Don">Don</a>, <a href="/player/ira">ira</a>!`,
		"xira ira_":   `xira ira_`,
		"a<b & [x]":   `<a href="/player/a%3Cb">a&lt;b</a> &amp; <a href="/player/%5Bx%5D">[x]</a>`,
		"<script>":    `&lt;script&gt;`,
		"Їхні ira":    `Їхні <a href="/player/ira">ira</a>`,
	} {
		if got := string(linkNames(text, players)); got != want {
			t.Errorf("linkNames(%q)\n got %s\nwant %s", text, got, want)
		}
	}
}

func TestAsciiTable(t *testing.T) {
	got := string(asciiTable([]int{4, 3},
		[]Cell{{Text: "ім'я", Class: "t-hi"}, {Text: "рів", Class: "t-hi"}},
		[][]Cell{
			{{Text: "ira", URL: "/player/ira"}, {Text: "7", Right: true}},
			{{Text: "nullbyte"}, {Text: "<b>", Class: "t-bad"}},
		}))
	want := "+------+-----+\n" +
		`| <span class="t-hi">ім&#39;я</span> | <span class="t-hi">рів</span> |` + "\n" +
		"+------+-----+\n" +
		`| <a href="/player/ira">ira</a>  |   <span>7</span> |` + "\n" +
		`| <span>nul…</span> | <span class="t-bad">&lt;b&gt;</span> |` + "\n" +
		"+------+-----+"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestPathEscape(t *testing.T) {
	for name, want := range map[string]string{
		"ira":     "ira",
		"a.b-c_d": "a.b-c_d",
		"[x]|^":   "%5Bx%5D%7C%5E",
		"ім'я":    "%D1%96%D0%BC%27%D1%8F",
		"a/b c":   "a%2Fb%20c",
	} {
		if got := pathEscape(name); got != want {
			t.Errorf("pathEscape(%q) = %q, want %q", name, got, want)
		}
	}
}

func get(t *testing.T, h http.Handler, path string) *http.Response {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec.Result()
}

func site(t *testing.T) *Site {
	t.Helper()
	s, err := NewSite()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStatic(t *testing.T) {
	s := site(t)
	h := s.Handler(func() *Game { return nil }, func() time.Time { return now })
	css := s.urls["style.css"]
	if !regexp.MustCompile(`^/static/style\.[0-9a-f]{10}\.css$`).MatchString(css) {
		t.Fatalf("the stylesheet is at %q", css)
	}
	r := get(t, h, css)
	body, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 || r.Header.Get("Content-Type") != "text/css; charset=utf-8" ||
		!strings.Contains(r.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("%s: %d %v", css, r.StatusCode, r.Header)
	}
	if !strings.Contains(string(body), "url("+s.urls["fonts/unbounded-900.woff2"]+")") {
		t.Error("the stylesheet does not point at the font's hashed URL")
	}
	if r := get(t, h, "/static/style.css"); r.StatusCode != 404 {
		t.Errorf("an unhashed URL: %d, want 404", r.StatusCode)
	}
	if r := get(t, h, s.urls["fonts/unbounded-900.woff2"]); r.Header.Get("Content-Type") != "font/woff2" {
		t.Errorf("a font is served as %q", r.Header.Get("Content-Type"))
	}
}

func TestGameJSON(t *testing.T) {
	g := load(t, "type2")
	h := site(t).Handler(func() *Game { return g }, func() time.Time { return now })
	r := get(t, h, "/game.json")
	body, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 || r.Header.Get("Content-Type") != "application/json" || !bytes.Equal(body, g.JSON) {
		t.Errorf("/game.json: %d %v, not the file", r.StatusCode, r.Header)
	}
	none := site(t).Handler(func() *Game { return nil }, func() time.Time { return now })
	if r := get(t, none, "/game.json"); r.StatusCode != 503 {
		t.Errorf("/game.json before the game is read: %d, want 503", r.StatusCode)
	}
}

var staticHash = regexp.MustCompile(`\.[0-9a-f]{10}\.`)

// TestPages renders each page for each variant in testdata and compares it
// with testdata/golden/<variant>-<page>.html. go test -update rewrites them;
// check the diff before committing.
func TestPages(t *testing.T) {
	type page struct {
		name, path string
		status     int
	}
	common := []page{{"index", "/", 200}, {"players", "/players", 200}, {"map", "/map", 200}}
	// the pages that look the same in every variant, only in two
	players := []page{{"player-Paganel", "/player/Paganel", 200}, {"player-Don", "/player/Don", 200},
		{"player-missing", "/player/nobody", 404}}
	s := site(t)
	for _, variant := range []string{"type2", "type1", "noquest", "stale", "nodata"} {
		var g *Game
		if variant != "nodata" {
			g = load(t, variant)
		}
		pages := common
		if variant == "type2" || variant == "nodata" {
			pages = append(slices.Clone(common), players...)
		}
		h := s.Handler(func() *Game { return g }, func() time.Time { return now })
		for _, p := range pages {
			r := get(t, h, p.path)
			body, _ := io.ReadAll(r.Body)
			// without the game, nobody can be told missing
			if want := p.status; r.StatusCode != want && (g != nil || r.StatusCode != 200) {
				t.Errorf("%s %s: %d, want %d", variant, p.path, r.StatusCode, want)
			}
			// so that a change to a static file doesn't change every page
			body = staticHash.ReplaceAll(body, []byte(".HASH."))
			golden := filepath.Join("testdata", "golden", variant+"-"+p.name+".html")
			if *update {
				if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, body, 0o644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(body, want) {
				t.Errorf("%s %s differs from %s; go test -update rewrites it", variant, p.path, golden)
			}
		}
	}
}

func TestPlayerURLs(t *testing.T) {
	g, err := parseGame([]byte(`{"updated": "2026-10-21T01:45:00Z", "players": [
		{"name": "Paganel", "level": 1}, {"name": "a/b [c]", "level": 2}]}`))
	if err != nil {
		t.Fatal(err)
	}
	h := site(t).Handler(func() *Game { return g }, func() time.Time { return now })
	for path, want := range map[string]struct {
		status int
		text   string
	}{
		"/player/":                {404, "Не вказано ім’я гравця."},
		"/player/paganel":         {200, "<h1>Paganel</h1>"},
		"/player/a%2Fb%20%5Bc%5D": {200, "<h1>a/b [c]</h1>"},
		"/player/%3Cscript%3E":    {404, "персонажа «&lt;script&gt;»"},
	} {
		r := get(t, h, path)
		body, _ := io.ReadAll(r.Body)
		if r.StatusCode != want.status || !strings.Contains(string(body), want.text) {
			t.Errorf("%s: %d, want %d with %q", path, r.StatusCode, want.status, want.text)
		}
	}
	if got := playerURL("a/b [c]"); got != "/player/a%2Fb%20%5Bc%5D" {
		t.Errorf("the URL of a/b [c] is %s", got)
	}
}
