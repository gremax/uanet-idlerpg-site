package main

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // the image has no zoneinfo
	"unicode/utf8"
)

//go:embed templates static
var files embed.FS

// Site holds the parsed templates and the static files under their
// content-hashed URLs.
type Site struct {
	pages  map[string]*template.Template
	urls   map[string]string // static file name: its URL
	assets map[string]asset  // URL: the file
}

type asset struct {
	body []byte
	typ  string
}

var types = map[string]string{
	".css":   "text/css; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".woff2": "font/woff2",
	".txt":   "text/plain; charset=utf-8",
}

func NewSite() (*Site, error) {
	s := &Site{pages: map[string]*template.Template{}, urls: map[string]string{},
		assets: map[string]asset{}}
	if err := s.loadStatic(); err != nil {
		return nil, err
	}
	layout, err := template.New("").Funcs(s.funcs()).ParseFS(files, "templates/layout.html", "templates/partials/*.html")
	if err != nil {
		return nil, err
	}
	names, err := fs.Glob(files, "templates/*.html")
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if name == "templates/layout.html" {
			continue
		}
		t, err := template.Must(layout.Clone()).ParseFS(files, name)
		if err != nil {
			return nil, err
		}
		s.pages[strings.TrimSuffix(path.Base(name), ".html")] = t
	}
	return s, nil
}

var cssURL = regexp.MustCompile(`url\(([^)]+)\)`)

// loadStatic gives every file in static/ a URL with a hash of its content,
// /static/<name>.<hash>.<ext>, so it can be cached for good. The CSS refers to
// the fonts by name, so they get their URLs first and the CSS is rewritten.
func (s *Site) loadStatic() error {
	var names []string
	err := fs.WalkDir(files, "static", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			names = append(names, strings.TrimPrefix(p, "static/"))
		}
		return err
	})
	if err != nil {
		return err
	}
	// the stylesheet last, after the files it points at
	slices.SortStableFunc(names, func(a, b string) int {
		return boolInt(path.Ext(a) == ".css") - boolInt(path.Ext(b) == ".css")
	})
	for _, name := range names {
		body, err := files.ReadFile("static/" + name)
		if err != nil {
			return err
		}
		if path.Ext(name) == ".css" {
			var missing []string
			body = cssURL.ReplaceAllFunc(body, func(m []byte) []byte {
				ref := path.Join(path.Dir(name), string(cssURL.FindSubmatch(m)[1]))
				if u, ok := s.urls[ref]; ok {
					return []byte("url(" + u + ")")
				}
				missing = append(missing, ref)
				return m
			})
			if missing != nil {
				return fmt.Errorf("static/%s refers to missing %v", name, missing)
			}
		}
		sum := sha256.Sum256(body)
		ext := path.Ext(name)
		u := "/static/" + strings.TrimSuffix(name, ext) + "." + hex.EncodeToString(sum[:5]) + ext
		s.urls[name] = u
		s.assets[u] = asset{body: body, typ: types[ext]}
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Site) funcs() template.FuncMap {
	return template.FuncMap{
		"asset": func(name string) (string, error) {
			if u, ok := s.urls[name]; ok {
				return u, nil
			}
			return "", fmt.Errorf("no static file %s", name)
		},
		"duration":  duration,
		"kyivTime":  kyivTime,
		"kyivDate":  kyivDate,
		"elapsed":   elapsed,
		"playerURL": playerURL,
		"iso":       func(t time.Time) string { return t.UTC().Format(time.RFC3339) },
		"podium":    Podium,
		"inc":       func(i int) int { return i + 1 },
		"slotName":  slotName,
		"at":        at,
		"quarterOf": quarterOf,
		"f1":        f1,
		// the land for the "terrain" template: with its names for the big
		// maps, bare for the small one
		"terrain":     func() terrainView { return terrainView{terrain, true} },
		"bareTerrain": func() terrainView { return terrainView{terrain, false} },
	}
}

type terrainView struct {
	Terrain *Terrain
	Labels  bool
}

// Page is what a page template gets.
type Page struct {
	Name string // the template, and the tab marked as current
	Game *Game  // nil when public.json has not been read yet
	Now  time.Time
	Data any // what the page shows besides the game
}

func (p *Page) Stale() bool {
	return p.Game != nil && p.Game.Stale(p.Now)
}

// Age is how long ago the bot wrote the game.
func (p *Page) Age() time.Duration {
	return p.Now.Sub(p.Game.Updated)
}

func (p *Page) Left(pl *Player) int64 {
	return p.Game.Left(pl, p.Now)
}

// Ticking says whether the player's timer counts down on the page: only
// while they are online and the bot keeps writing the file.
func (p *Page) Ticking(pl *Player) bool {
	return pl.Online && !p.Stale()
}

// Linked is the text in pieces, for the "linked" template to make every
// player's name a link to their page.
func (p *Page) Linked(text string) []Piece {
	return linkNames(text, p.Game.Players)
}

// Tabs are the sections in the header, in both views.
type Tab struct{ Name, URL, Paper, Term string }

var tabs = []Tab{
	{"index", "/", "Головна", "[1] main"},
	{"players", "/players", "Гравці", "[2] players"},
	{"map", "/map", "Мапа", "[3] map"},
	{"quest", "/quest", "Квест", "[4] quest"},
}

func (p *Page) Tabs() []Tab { return tabs }

// Tab is the tab marked as current: a player's page is under the players.
func (p *Page) Tab() string {
	if p.Name == "player" {
		return "players"
	}
	return p.Name
}

func (s *Site) Render(name string, p *Page) ([]byte, error) {
	t, ok := s.pages[name]
	if !ok {
		return nil, fmt.Errorf("no page %s", name)
	}
	p.Name = name
	var b bytes.Buffer
	if err := t.ExecuteTemplate(&b, "layout", p); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func playerURL(name string) string {
	return "/player/" + pathEscape(name)
}

// pathEscape escapes a name for one path segment. url.PathEscape leaves
// characters like [ ] | ^ alone that are fine in a path but that some
// clients still escape, so names round-trip the same either way.
func pathEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// isNick says whether c can be part of an IRC nick, so "ira" is not found
// inside "irate".
func isNick(c rune) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' ||
		strings.ContainsRune("_-[]\\^{}|`", c)
}

// Piece is a piece of text: a player's name, or anything else.
type Piece struct {
	Text   string
	Player bool
}

// linkNames cuts text into pieces at the names of the players.
func linkNames(text string, players []*Player) []Piece {
	names := make([]string, 0, len(players))
	for _, p := range players {
		if p.Name != "" {
			names = append(names, p.Name)
		}
	}
	// the longest first, so "Don" does not take the start of "Donna"
	slices.SortFunc(names, func(a, b string) int { return len(b) - len(a) })
	var pieces []Piece
	add := func(text string, player bool) {
		if text != "" {
			pieces = append(pieces, Piece{text, player})
		}
	}
	last := 0
	prev := rune(0)
	for i, c := range text {
		if i < last {
			prev = c
			continue
		}
		if !isNick(prev) {
			for _, n := range names {
				if !strings.HasPrefix(text[i:], n) {
					continue
				}
				if next, _ := utf8.DecodeRuneInString(text[i+len(n):]); isNick(next) {
					continue
				}
				add(text[last:i], false)
				add(n, true)
				last = i + len(n)
				break
			}
		}
		prev = c
	}
	add(text[last:], false)
	return pieces
}

// plural picks the Ukrainian form for n: plural(5, "день", "дні", "днів").
func plural(n int64, one, few, many string) string {
	n %= 100
	if n < 0 {
		n = -n
	}
	switch {
	case n > 10 && n < 20:
		return many
	case n%10 == 1:
		return one
	case n%10 >= 2 && n%10 <= 4:
		return few
	}
	return many
}

// duration is "1 день, 02:03:04", the way the bot writes times.
func duration(seconds int64) string {
	s := max(0, seconds)
	days := s / 86400
	return fmt.Sprintf("%d %s, %02d:%02d:%02d", days, plural(days, "день", "дні", "днів"),
		s%86400/3600, s%3600/60, s%60)
}

var kyiv = mustZone("Europe/Kyiv")

func mustZone(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

// kyivTime is "04:45" in Kyiv.
func kyivTime(t time.Time) string {
	return t.In(kyiv).Format("15:04")
}

var months = [...]string{"січня", "лютого", "березня", "квітня", "травня", "червня",
	"липня", "серпня", "вересня", "жовтня", "листопада", "грудня"}

// kyivDate is "21 жовтня 2026 р. о 04:45" in Kyiv.
func kyivDate(t time.Time) string {
	t = t.In(kyiv)
	return fmt.Sprintf("%d %s %d р. о %s", t.Day(), months[t.Month()-1], t.Year(), t.Format("15:04"))
}

// elapsed is "2 хв", "3 години" or "2 дні": how long, without the "тому".
func elapsed(d time.Duration) string {
	minutes := int64(max(0, d) / time.Minute)
	if minutes < 60 {
		return fmt.Sprintf("%d хв", minutes)
	}
	hours := minutes / 60
	if hours < 48 {
		return fmt.Sprintf("%d %s", hours, plural(hours, "годину", "години", "годин"))
	}
	days := hours / 24
	return fmt.Sprintf("%d %s", days, plural(days, "день", "дні", "днів"))
}

// Cell is one cell of an ASCII table: text in a terminal colour class,
// optionally a link.
type Cell struct {
	Text  string
	Class string
	URL   string
	Right bool   // pad on the left
	Left  *int64 // a timer: the seconds left, which app.js counts down
}

// Table is an ASCII table laid out the way a terminal program prints one,
// for the "ascii-table" template:
//
//	+------+-----+
//	| name | lvl |
//	+------+-----+
//	| ira  |  23 |
//	+------+-----+
type Table struct {
	Rule   string
	Header []PaddedCell
	Rows   [][]PaddedCell
}

// PaddedCell is a cell cut to its column, and the spaces that fill the rest.
// A timer keeps its spaces inside, for app.js to keep the width as the time
// changes; a link keeps them outside, so only the text is underlined.
type PaddedCell struct {
	Cell
	Width         int
	Before, After string
}

// asciiTable lays out a table with columns as wide as widths says: longer
// text is cut with "…".
func asciiTable(widths []int, header []Cell, rows [][]Cell) Table {
	t := Table{Rule: "+"}
	for _, w := range widths {
		t.Rule += strings.Repeat("-", w+2) + "+"
	}
	row := func(cells []Cell) []PaddedCell {
		padded := make([]PaddedCell, len(cells))
		for i, c := range cells {
			c.Text = fit(c.Text, widths[i])
			p := PaddedCell{Cell: c, Width: widths[i]}
			pad := strings.Repeat(" ", widths[i]-utf8.RuneCountInString(c.Text))
			switch {
			case c.Left != nil:
				p.Text += pad
			case c.Right:
				p.Before = pad
			default:
				p.After = pad
			}
			padded[i] = p
		}
		return padded
	}
	t.Header = row(header)
	for _, r := range rows {
		t.Rows = append(t.Rows, row(r))
	}
	return t
}

// fit cuts text to width characters, with "…" in place of the rest.
func fit(text string, width int) string {
	if utf8.RuneCountInString(text) <= width {
		return text
	}
	r := []rune(text)
	return string(r[:width-1]) + "…"
}

// eventsShown is how many of the latest events the home page lists.
const eventsShown = 15

type indexView struct {
	Leader    *Player
	Events    []Event
	Penalties Table
}

func indexData(p *Page, _ *http.Request) (any, bool) {
	v := indexView{Penalties: penaltiesTable}
	if g := p.Game; g != nil {
		if len(g.Humans) > 0 {
			v.Leader = g.Humans[0]
		}
		v.Events = g.Events[:min(len(g.Events), eventsShown)]
	}
	return v, true
}

var penaltiesTable = func() Table {
	rows := [][2]string{
		{"репліка", "довжина × 1.14^рівень"},
		{"зміна ніку", "30с × 1.14^рівень"},
		{"вихід з каналу", "200с × 1.14^рівень"},
		{"кік", "250с × 1.14^рівень"},
		{"quit", "20с × 1.14^рівень"},
		{"logout", "20с × 1.14^рівень"},
		{"провал квесту", "штраф усім у грі"},
	}
	var cells [][]Cell
	for i, r := range rows {
		class := "t-fg"
		if i == len(rows)-1 {
			class = "t-bad"
		}
		cells = append(cells, []Cell{{Text: r[0], Class: "t-fg"}, {Text: r[1], Class: class}})
	}
	return asciiTable([]int{16, 30}, heads("дія", "штраф"), cells)
}()

// alignments are the names of g, n and e: in full, short, and the chip's class.
var alignments = map[string][3]string{
	"g": {"добро", "добро", "good"},
	"n": {"нейтральність", "нейтр.", "neutral"},
	"e": {"зло", "зло", "evil"},
}

func (p *Player) alignment() [3]string {
	if a, ok := alignments[p.Alignment]; ok {
		return a
	}
	return [3]string{"?", "?", "neutral"}
}

func (p *Player) AlignmentName() string  { return p.alignment()[0] }
func (p *Player) AlignmentShort() string { return p.alignment()[1] }
func (p *Player) AlignmentClass() string { return p.alignment()[2] }

// AlignmentColour is the terminal's colour for the alignment: good green,
// evil red, else neutral.
func (p *Player) AlignmentColour(neutral string) string {
	switch p.Alignment {
	case "g":
		return "t-ok"
	case "e":
		return "t-bad"
	}
	return neutral
}

func (p *Player) StatusWord() string {
	if p.Online {
		return "online"
	}
	return "offline"
}

func (p *Player) StatusColour() string {
	if p.Online {
		return "t-ok"
	}
	return "t-dim"
}

// timer is a table cell counting down to the player's next level, or the
// time standing still while it does not tick.
func (p *Page) timer(pl *Player, class string) Cell {
	left := p.Left(pl)
	c := Cell{Text: duration(left), Class: class}
	if p.Ticking(pl) {
		c.Left = &left
	}
	return c
}

var podium = []string{"first", "second", "third"}

// Podium is the class of the row for the rank, if it's one of the first three.
func Podium(rank int) string {
	if rank >= 1 && rank <= len(podium) {
		return "podium " + podium[rank-1]
	}
	return ""
}

type playersView struct {
	Ranking, NPCs Table // the terminal's tables
}

func playersData(p *Page, _ *http.Request) (any, bool) {
	g := p.Game
	if g == nil {
		return playersView{}, true
	}
	var v playersView
	var rows [][]Cell
	for i, pl := range g.Humans {
		rank := Cell{Text: fmt.Sprint(i + 1), Class: "t-fg", Right: true}
		if i < len(podium) {
			rank.Class = "t-gold"
		}
		rows = append(rows, []Cell{rank,
			{Text: pl.Name, URL: playerURL(pl.Name)},
			{Text: fmt.Sprint(pl.Level), Class: "t-hi", Right: true},
			{Text: pl.Class},
			p.timer(pl, "t-fg"),
			{Text: pl.AlignmentShort(), Class: pl.AlignmentColour("t-fg")},
			{Text: pl.StatusWord(), Class: pl.StatusColour()},
		})
	}
	v.Ranking = asciiTable([]int{3, 14, 3, 18, 18, 6, 7}, heads("#", "гравець", "рів", "клас", "до рівня", "світ.", "статус"), rows)
	rows = nil
	for _, pl := range g.NPCs {
		if pl.Stub {
			rows = append(rows, []Cell{{Text: pl.Name, Class: "t-gold"},
				{Text: "-", Class: "t-fg"}, {Text: "-", Class: "t-fg"}, {Text: "-", Class: "t-fg"}})
			continue
		}
		rows = append(rows, []Cell{{Text: pl.Name, URL: playerURL(pl.Name)},
			{Text: fmt.Sprint(pl.Level), Class: "t-hi", Right: true},
			{Text: pl.Class},
			{Text: pl.StatusWord(), Class: pl.StatusColour()},
		})
	}
	v.NPCs = asciiTable([]int{12, 3, 20, 7}, heads("npc", "рів", "клас", "статус"), rows)
	return v, true
}

// heads are the header cells of a terminal table.
func heads(names ...string) []Cell {
	cells := make([]Cell, len(names))
	for i, n := range names {
		cells[i] = Cell{Text: n, Class: "t-hi"}
	}
	return cells
}

// slots are the item slots, in the order the bot lists them, and their names.
var slots = [][2]string{
	{"amulet", "амулет"},
	{"charm", "оберіг"},
	{"helm", "шолом"},
	{"boots", "чоботи"},
	{"gloves", "рукавиці"},
	{"ring", "каблучка"},
	{"leggings", "поножі"},
	{"shield", "щит"},
	{"tunic", "туніка"},
	{"weapon", "зброя"},
}

func slotName(slot string) string {
	for _, s := range slots {
		if s[0] == slot {
			return s[1]
		}
	}
	return slot
}

type mapView struct {
	Markers Overlay
	ASCII   []Run
	// the terminal's tables: where everyone and everything is, and the places
	Where, Places Table
}

func at(x, y int) string { return fmt.Sprintf("[%d, %d]", x, y) }

func everyone(*Player) bool { return true }

func mapData(p *Page, _ *http.Request) (any, bool) {
	var v mapView
	g := p.Game
	if g == nil {
		return v, true
	}
	v.Markers = Markers{}.For(g)
	v.ASCII = terrain.ASCII(g, everyone)
	var rows [][]Cell
	for _, pl := range g.Players {
		status := pl.StatusWord()
		if pl.NPC {
			status = "npc"
		}
		rows = append(rows, []Cell{{Text: pl.Name, URL: playerURL(pl.Name)}, {Text: at(pl.X, pl.Y)},
			{Text: quarterOf(pl.X, pl.Y)}, {Text: status, Class: pl.StatusColour()}})
	}
	for _, it := range g.MapItems {
		rows = append(rows, []Cell{{Text: fmt.Sprintf("%s %d", slotName(it.Type), it.Level), Class: "t-bad"},
			{Text: at(it.X, it.Y)}, {Text: quarterOf(it.X, it.Y)}, {Text: "на землі"}})
	}
	v.Where = asciiTable([]int{14, 10, 15, 8}, heads("що", "де", "частина", "стан"), rows)
	rows = nil
	for i, pl := range append([]Place{capital}, places...) {
		class := "t-hi"
		if i == 0 {
			class = "t-gold"
		}
		rows = append(rows, []Cell{{Text: pl.Name, Class: class}, {Text: at(pl.X, pl.Y)}, {Text: quarterOf(pl.X, pl.Y)}})
	}
	v.Places = asciiTable([]int{26, 10, 15}, heads("місце", "де", "частина"), rows)
	return v, true
}

// uniques are the unique items by their letter in bot.pl. The names must
// match the ones the bot announces, from its lang/uk.txt.
var uniques = map[string]string{
	"a": "Всевидюча Велика Корона Маттта",
	"b": "Захисний Латний Обладунок Res0",
	"c": "Штормовий Магічний Амулет Двіна",
	"d": "Колосальний Меч Люті Йотуна",
	"e": "Ціпок Сліпої Люті Дрдінка",
	"f": "Чарівні Чоботи Прудкості Мрквіка",
	"g": "Молот Тями Джеффа",
	"h": "Сяйлива Каблучка Блиску Джульєтти",
}

func uniqueName(letter string) string {
	if n, ok := uniques[letter]; ok {
		return n
	}
	return "унікальний предмет «" + letter + "»"
}

// penaltyKinds are the kinds of penalty, in the order the pages list them.
var penaltyKinds = [][2]string{
	{"mesg", "репліки"},
	{"nick", "зміна ніку"},
	{"part", "вихід з каналу"},
	{"kick", "кік"},
	{"quit", "quit"},
	{"quest", "провал квесту"},
	{"logout", "logout"},
}

type item struct {
	Name   string // of the slot
	Level  int
	Unique string // the unique item's name, if it is one
}

type penalty struct {
	Name    string
	Seconds int64
}

type playerView struct {
	Wanted       string  // the name in the URL
	Player       *Player // nil if there is none by that name
	Rank         int
	Items        []item
	ItemSum      int
	Penalties    []penalty
	PenaltyTotal int64
	Minimap      Overlay
	// the terminal's: the label of the timer, dotted out, and two tables
	NextLabel                string
	TermItems, TermPenalties Table
}

func playerData(p *Page, r *http.Request) (any, bool) {
	v := playerView{Wanted: r.PathValue("name")}
	g := p.Game
	if g == nil {
		return v, true
	}
	if v.Player = g.Find(v.Wanted); v.Player == nil {
		return v, false
	}
	pl := v.Player
	v.Rank = g.Rank(pl)
	var rows [][]Cell
	for _, s := range slots {
		it := item{Name: s[1], Level: pl.Items[s[0]]}
		levelClass := "t-hi"
		if letter := pl.Unique[s[0]]; letter != "" {
			it.Unique = uniqueName(letter)
			levelClass = "t-gold"
		}
		v.Items = append(v.Items, it)
		v.ItemSum += it.Level
		rows = append(rows, []Cell{{Text: it.Name}, {Text: fmt.Sprint(it.Level), Class: levelClass, Right: true},
			{Text: it.Unique, Class: "t-gold"}})
	}
	rows = append(rows, []Cell{{Text: "сума", Class: "t-hi"}, {Text: fmt.Sprint(v.ItemSum), Class: "t-gold", Right: true}, {}})
	v.TermItems = asciiTable([]int{9, 4, 34}, heads("слот", "рів", "унікальний"), rows)

	rows = nil
	for _, k := range penaltyKinds {
		pen := penalty{Name: k[1], Seconds: pl.Penalties[k[0]]}
		v.Penalties = append(v.Penalties, pen)
		v.PenaltyTotal += pen.Seconds
		class := "t-dim"
		if pen.Seconds > 0 {
			class = "t-bad"
		}
		rows = append(rows, []Cell{{Text: pen.Name}, {Text: duration(pen.Seconds), Class: class, Right: true}})
	}
	rows = append(rows, []Cell{{Text: "разом", Class: "t-hi"}, {Text: duration(v.PenaltyTotal), Class: "t-gold", Right: true}})
	v.TermPenalties = asciiTable([]int{15, 18}, heads("тип", "час"), rows)

	v.NextLabel = fmt.Sprintf("до рівня %d", pl.Level+1)
	v.NextLabel += strings.Repeat(".", max(0, 20-utf8.RuneCountInString(v.NextLabel)))
	v.Minimap = Markers{Small: true, Highlight: pl.Name}.For(g)
	return v, true
}

type quester struct {
	Name   string
	Player *Player // nil if the bot has no such character
}

type questView struct {
	Quest    *Quest
	Questers []quester
	// a quest to walk: the names of its points, and the maps with the
	// questers only
	Place1, Place2 string
	Markers        Overlay
	ASCII          []Run
	// a quest for a time: the seconds left
	EndsIn int64
}

func questData(p *Page, _ *http.Request) (any, bool) {
	var v questView
	g := p.Game
	if g == nil || g.Quest == nil {
		return v, true
	}
	q := g.Quest
	v.Quest = q
	for _, name := range q.Players {
		v.Questers = append(v.Questers, quester{name, g.Find(name)})
	}
	if q.Type == 2 {
		v.Place1, v.Place2 = placeAt(q.P1[0], q.P1[1]), placeAt(q.P2[0], q.P2[1])
		questing := func(pl *Player) bool { return slices.Contains(q.Players, pl.Name) }
		v.Markers = Markers{Show: questing}.For(g)
		v.ASCII = terrain.ASCII(g, questing)
	} else {
		v.EndsIn = max(0, int64(q.Ends.Sub(p.Now)/time.Second))
	}
	return v, true
}
