package main

import (
	"fmt"
	"html"
	"html/template"
	"math"
	"math/big"
	"slices"
	"strings"
)

// The world: a 500 × 500 realm, small y is north. The land is made up, drawn
// once at startup from a fixed seed; the pages add the players, the items and
// the quest on top.

const worldSize = 500

// Place is a named point: the capital, or one the quests in the bot's
// events.uk.txt send players to.
type Place struct {
	X, Y  int
	Name  string
	Label string // where the name goes on the big map: right, left, top, bottom, or nowhere
}

var capital = Place{X: 325, Y: 270, Name: "Столиця"}

var places = []Place{
	{225, 315, "Вежі Анкх-Аллора", "top"},
	{280, 360, "лігво чаклуна Крука", "right"},
	{400, 375, "темні землі Т'рнальвфа", ""},
	{480, 380, "темні землі Т'рнальвфа", ""},
	{290, 65, "троль Докт'р Вііі", "right"},
	{480, 315, "місто Вельврах", "left"},
	{70, 315, "таємний хід до Бхараша", "right"},
	{50, 350, "Великі гори Шахліль", ""},
	{167, 358, "лігво Грабтула", "right"},
	{160, 380, "вовчі пагорби", "bottom"},
	{35, 40, "табір армії Данії", "right"},
	{235, 125, "перевал", "left"},
	{430, 60, "країна Квок", "top"},
	{155, 155, "плем'я Джоу Ботзі", "right"},
	{70, 125, "зомбі Джоу Ботзі", "top"},
	{170, 100, "зомбі Джоу Ботзі", "right"},
	{370, 325, "архів логів", "bottom"},
	{270, 130, "архів логів", "top"},
	{155, 40, "комп'ютер старого адміна", "bottom"},
	{325, 130, "комп'ютер старого адміна", "right"},
}

// quarterOf names the part of the realm (x, y) is in, split the way bot.pl's
// war() splits it.
func quarterOf(x, y int) string {
	q := "Південний "
	if y < 250 {
		q = "Північний "
	}
	if x >= 250 {
		return q + "Схід"
	}
	return q + "Захід"
}

// placeAt is the name of the capital or the quest place at exactly (x, y),
// or "".
func placeAt(x, y int) string {
	if x == capital.X && y == capital.Y {
		return capital.Name
	}
	for _, p := range places {
		if p.X == x && p.Y == y {
			return p.Name
		}
	}
	return ""
}

type circle struct{ x, y, r float64 }

func (c circle) has(x, y float64) bool {
	return (x-c.x)*(x-c.x)+(y-c.y)*(y-c.y) <= c.r*c.r
}

// The land, laid out around the quest places.
var (
	lake      = circle{425, 175, 36}
	river     = [][2]float64{{0, 70}, {70, 95}, {140, 150}, {190, 230}, {250, 275}, {330, 300}, {400, 360}, {455, 430}, {500, 470}}
	forests   = []circle{{85, 178, 50}, {300, 197, 30}}
	mountains = circle{58, 345, 42}
	hills     = circle{160, 390, 22}
	darkLands = circle{440, 385, 55}
	swamp     = circle{265, 455, 36}
)

type mapText struct {
	text   string
	x, y   int
	anchor string
}

var landNames = []mapText{
	{"Синє Озеро", 425, 179, "middle"},
	{"Старий Ліс", 85, 226, "middle"},
	{"Тихий Гай", 300, 240, "middle"},
	{"Туманні Болота", 265, 496, "middle"},
	{"Великі гори Шахліль", 8, 416, "start"},
	{"Темні землі Т'рнальвфа", 492, 457, "end"},
}

var quarterNames = []mapText{
	{"ПІВНІЧНИЙ ЗАХІД", 7, 243, "start"},
	{"ПІВНІЧНИЙ СХІД", 493, 243, "end"},
	{"ПІВДЕННИЙ ЗАХІД", 7, 266, "start"},
	{"ПІВДЕННИЙ СХІД", 493, 266, "end"},
}

// where a place's name sits relative to its point
var labelOffset = map[string]struct {
	dx, dy int
	anchor string
}{
	"right":  {9, 4, "start"},
	"left":   {-9, 4, "end"},
	"top":    {0, -10, "middle"},
	"bottom": {0, 17, "middle"},
}

const (
	ink  = "#14123a"
	halo = `stroke="#fff8e3" stroke-width="4" paint-order="stroke"`
)

// random is Park–Miller from seed: the same sequence on every run.
func random(seed int64) func() float64 {
	return func() float64 {
		seed = seed * 16807 % 2147483647
		return float64(seed-1) / 2147483646
	}
}

// pointIn is a uniformly random point within radius of (x, y).
func pointIn(rand func() float64, c circle) (float64, float64) {
	angle := rand() * math.Pi * 2
	distance := math.Sqrt(rand()) * c.r
	return c.x + math.Cos(angle)*distance, c.y + math.Sin(angle)*distance
}

func riverDistance(x, y float64) float64 {
	best := math.Inf(1)
	for i := 0; i < len(river)-1; i++ {
		ax, ay := river[i][0], river[i][1]
		bx, by := river[i+1][0], river[i+1][1]
		dx, dy := bx-ax, by-ay
		t := max(0, min(1, ((x-ax)*dx+(y-ay)*dy)/(dx*dx+dy*dy)))
		best = min(best, math.Hypot(ax+t*dx-x, ay+t*dy-y))
	}
	return best
}

func terrainAt(x, y float64) string {
	switch {
	case lake.has(x, y) || riverDistance(x, y) < 11:
		return "water"
	case mountains.has(x, y):
		return "mountains"
	case hills.has(x, y):
		return "hills"
	case darkLands.has(x, y):
		return "dark"
	case swamp.has(x, y):
		return "swamp"
	case slices.ContainsFunc(forests, func(f circle) bool { return f.has(x, y) }):
		return "forest"
	}
	return ""
}

// f1 is x with one decimal, rounding a tie up, as JavaScript's toFixed(1)
// does, so the map is the one tools/map.mjs drew for the mockup.
func f1(x float64) string {
	exact := new(big.Float).SetFloat64(x).Text('f', 80)
	whole, frac, _ := strings.Cut(exact, ".")
	n := new(big.Int)
	n.SetString(whole+frac[:1], 10)
	if frac[1] >= '5' {
		n.Add(n, big.NewInt(1))
	}
	s := fmt.Sprintf("%02s", n.String())
	return s[:len(s)-1] + "." + s[len(s)-1:]
}

// hatch is hatching across a circle as plain strokes, 7 apart at 45°.
func hatch(c circle) string {
	const step = 7.0
	offset := (c.x+c.y)/math.Sqrt2 - 0.5
	var b strings.Builder
	for k := math.Ceil((offset - c.r) / step); k*step < offset+c.r; k++ {
		d := offset - k*step
		half := math.Sqrt(c.r*c.r-d*d) - 1
		if half <= 0 {
			continue
		}
		fx, fy := c.x-d/math.Sqrt2, c.y-d/math.Sqrt2
		ex := half / math.Sqrt2
		fmt.Fprintf(&b, "M%s %sL%s %s", f1(fx+ex), f1(fy-ex), f1(fx-ex), f1(fy+ex))
	}
	return b.String()
}

// Terrain is the map's land drawn once: for the paper map, three groups of
// SVG to put in an <svg viewBox="0 0 500 500"> (the small map on a player's
// page leaves out Labels), and for the terminal, a grid of glyphs.
type Terrain struct {
	Land, Labels, Places template.HTML
	grid                 [asciiRows][asciiColumns]rune
}

var terrain = drawTerrain(42)

func drawTerrain(seed int64) *Terrain {
	t := &Terrain{}
	t.Land, t.Labels, t.Places = drawSVG(seed)
	t.grid = drawGrid()
	return t
}

func drawSVG(seed int64) (land, labels, placed template.HTML) {
	rand := random(seed)
	var b strings.Builder
	add := func(format string, args ...any) {
		b.WriteString("  ")
		fmt.Fprintf(&b, format, args...)
		b.WriteString("\n")
	}

	add(`<rect width="%d" height="%d" fill="#fff8e3"/>`, worldSize, worldSize)
	var grid strings.Builder
	for g := 50; g < worldSize; g += 50 {
		if g != 250 {
			fmt.Fprintf(&grid, "M%d 0V%dM0 %dH%d", g, worldSize, g, worldSize)
		}
	}
	add(`<path d="%s" fill="none" stroke="#e7dfc2"/>`, grid.String())

	add(`<!-- dark lands -->`)
	add(`<circle cx="%g" cy="%g" r="%g" fill="#d9d2ea"/>`, darkLands.x, darkLands.y, darkLands.r)
	add(`<path d="%s" fill="none" stroke="#4b4870"/>`, hatch(darkLands))
	add(`<circle cx="%g" cy="%g" r="%g" fill="none" stroke="%s" stroke-width="2.5" stroke-dasharray="8 5"/>`, darkLands.x, darkLands.y, darkLands.r, ink)

	add(`<!-- swamp -->`)
	var reeds strings.Builder
	for range 60 {
		x, y := pointIn(rand, swamp)
		fmt.Fprintf(&reeds, "M%s %sh8", f1(x-4), f1(y))
	}
	add(`<path d="%s" fill="none" stroke="#6aa58a" stroke-width="2" stroke-linecap="round"/>`, reeds.String())

	add(`<!-- river and lake -->`)
	var points []string
	for _, p := range river {
		points = append(points, fmt.Sprintf("%g %g", p[0], p[1]))
	}
	riverPath := "M" + strings.Join(points, " L")
	add(`<path d="%s" fill="none" stroke="#bfe3ff" stroke-width="14" stroke-linecap="round" stroke-linejoin="round"/>`, riverPath)
	add(`<path d="%s" fill="none" stroke="#2338ff" stroke-opacity=".35" stroke-width="2" stroke-dasharray="6 6"/>`, riverPath)
	add(`<circle cx="%g" cy="%g" r="%g" fill="#bfe3ff" stroke="%s" stroke-width="3"/>`, lake.x, lake.y, lake.r, ink)

	add(`<!-- forests -->`)
	add(`<g fill="#3ddc9f" stroke="%s" stroke-width="1.5">`, ink)
	for _, f := range forests {
		for range int(math.Floor(f.r*0.9 + 0.5)) {
			x, y := pointIn(rand, f)
			if riverDistance(x, y) < 10 {
				continue
			}
			add(`  <circle cx="%s" cy="%s" r="%s"/>`, f1(x), f1(y), f1(5+rand()*3))
		}
	}
	add(`</g>`)

	add(`<!-- hills -->`)
	add(`<g fill="#e8e4d4" stroke="%s" stroke-width="2" stroke-linecap="round">`, ink)
	for range 9 {
		x, y := pointIn(rand, circle{hills.x, hills.y, hills.r - 6})
		add(`  <path d="M%s %s q9 -12 18 0"/>`, f1(x-9), f1(y))
	}
	add(`</g>`)

	add(`<!-- mountains -->`)
	add(`<g stroke="%s" stroke-linejoin="round">`, ink)
	for range 14 {
		x, base := pointIn(rand, circle{mountains.x, mountains.y, mountains.r - 8})
		y := base + 8
		h := 14 + rand()*10
		add(`  <path d="M%s %s L%s %s L%s %sZ" fill="#e8e4d4" stroke-width="2"/>`,
			f1(x-h*0.7), f1(y), f1(x), f1(y-h), f1(x+h*0.7), f1(y))
		add(`  <path d="M%s %s L%s %s L%s %s" fill="#fff" stroke-width="1.5"/>`,
			f1(x-h*0.22), f1(y-h*0.68), f1(x), f1(y-h), f1(x+h*0.22), f1(y-h*0.68))
	}
	add(`</g>`)

	add(`<!-- the four parts of the realm -->`)
	add(`<path d="M250 0 V%d M0 250 H%d" fill="none" stroke="%s" stroke-opacity=".45" stroke-width="2" stroke-dasharray="12 6"/>`, worldSize, worldSize, ink)
	land = template.HTML(b.String())

	// the places, the capital and the frame go over the names
	b.Reset()
	add(`<!-- quest places -->`)
	add(`<g fill="%s" stroke="%s" stroke-width="1.2">`, ink, ink)
	for _, p := range places {
		add(`  <g><title>%s</title><circle cx="%d" cy="%d" r="3.2" stroke="none"/><circle cx="%d" cy="%d" r="6" fill="none"/></g>`,
			escape(fmt.Sprintf("%s [%d, %d]", p.Name, p.X, p.Y)), p.X, p.Y, p.X, p.Y)
	}
	add(`</g>`)
	add(`<!-- the capital -->`)
	add(`<g transform="translate(%d %d)" stroke="%s">`, capital.X, capital.Y, ink)
	add(`  <title>%s [%d, %d]</title>`, capital.Name, capital.X, capital.Y)
	add(`  <path d="M-15 10 V-6 h5 v4 h4 v-4 h5 v4 h4 v-4 h5 v4 h4 v-4 h3 V10 Z" fill="#ffd43b" stroke-width="2.5" stroke-linejoin="round"/>`)
	add(`  <path d="M-3 10 v-7 a3 3 0 0 1 6 0 v7" fill="%s" stroke="none"/>`, ink)
	add(`  <path d="M0 -6 V-20 M0 -20 L9 -16.5 L0 -13" fill="#2338ff" stroke-width="2" stroke-linejoin="round"/>`)
	add(`  <text y="27" text-anchor="middle" font-family="Unbounded,Arial Black,sans-serif" font-weight="900" font-size="12" fill="%s" %s>%s</text>`, ink, halo, capital.Name)
	add(`</g>`)
	add(`<!-- coordinates -->`)
	add(`<g font-family="Ubuntu Mono,monospace" font-size="11" fill="#9b8f62">`)
	for c := 100; c < worldSize; c += 100 {
		add(`  <text x="%d" y="12">%d</text><text x="3" y="%d">%d</text>`, c+3, c, c-3, c)
	}
	add(`</g>`)
	add(`<rect x="1.5" y="1.5" width="497" height="497" fill="none" stroke="%s" stroke-width="3" rx="10"/>`, ink)
	placed = template.HTML(b.String())

	b.Reset()
	add(`<g font-size="10.5" font-weight="800" letter-spacing="1.5" fill="%s" fill-opacity=".6" %s>`, ink, halo)
	for _, q := range quarterNames {
		add(`  <text x="%d" y="%d" text-anchor="%s">%s</text>`, q.x, q.y, q.anchor, q.text)
	}
	add(`</g>`)
	add(`<g font-size="12.5" font-weight="700" font-style="italic" fill="#4b4870" %s>`, halo)
	for _, l := range landNames {
		add(`  <text x="%d" y="%d" text-anchor="%s">%s</text>`, l.x, l.y, l.anchor, escape(l.text))
	}
	add(`</g>`)
	add(`<g font-size="11" font-weight="600" fill="%s" %s>`, ink, halo)
	for _, p := range places {
		if p.Label == "" {
			continue
		}
		o := labelOffset[p.Label]
		add(`  <text x="%d" y="%d" text-anchor="%s">%s</text>`, p.X+o.dx, p.Y+o.dy, o.anchor, escape(p.Name))
	}
	add(`</g>`)
	labels = template.HTML(b.String())
	return land, labels, placed
}

// escape escapes text for SVG the way tools/map.mjs did: & and < only.
func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;").Replace(s)
}

// The terminal map: 50 × 25 cells, each 10 wide and 20 high.
const (
	asciiColumns = 50
	asciiRows    = 25
)

var glyphs = map[string]rune{"water": '~', "forest": '"', "mountains": '^', "hills": 'm', "dark": '%', "swamp": ','}

func drawGrid() (grid [asciiRows][asciiColumns]rune) {
	for row := range asciiRows {
		for col := range asciiColumns {
			land := terrainAt(float64(col*10+5), float64(row*20+10))
			glyph, ok := glyphs[land]
			switch {
			case ok:
			case col == 25 && row == 12:
				glyph = '+'
			case col == 25:
				glyph = '|'
			case row == 12:
				glyph = '-'
			default:
				glyph = '.'
			}
			grid[row][col] = glyph
		}
	}
	for _, p := range places {
		grid[p.Y/20][p.X/10] = '*'
	}
	grid[capital.Y/20][capital.X/10] = 'C'
	return grid
}

// the terminal's colours for the glyphs of the land
var glyphColours = map[rune]string{'"': "t-ok", '^': "t-hi", '%': "t-bad", '*': "t-hi", 'C': "t-gold", '+': "t-hi"}

// ASCII draws the terminal map with the game's markers: items, the quest's
// points, and the players for whom show is true. The frame and the land
// without a colour take the colour of the <pre> around it.
func (t *Terrain) ASCII(g *Game, show func(*Player) bool) template.HTML {
	type cell struct {
		glyph  rune
		colour string
	}
	var cells [asciiRows][asciiColumns]cell
	for r, row := range t.grid {
		for c, glyph := range row {
			cells[r][c] = cell{glyph, glyphColours[glyph]}
		}
	}
	put := func(x, y int, glyph rune, colour string) {
		cells[min(asciiRows-1, max(0, y/20))][min(asciiColumns-1, max(0, x/10))] = cell{glyph, colour}
	}
	for _, it := range g.MapItems {
		put(it.X, it.Y, '+', "t-bad")
	}
	if q := g.Quest; q != nil && q.Type == 2 {
		put(q.P1[0], q.P1[1], '1', "t-gold")
		put(q.P2[0], q.P2[1], '2', "t-gold")
	}
	for _, p := range g.Players {
		switch {
		case !show(p):
		case p.NPC:
			put(p.X, p.Y, 'n', "t-gold")
		case p.Online:
			put(p.X, p.Y, '@', "t-hi")
		default:
			put(p.X, p.Y, 'o', "")
		}
	}

	// one span per run of a colour
	var b strings.Builder
	colour, run := "", ""
	flush := func() {
		if colour == "" {
			b.WriteString(run)
		} else {
			fmt.Fprintf(&b, `<span class="%s">%s</span>`, colour, run)
		}
		run = ""
	}
	write := func(s, c string) {
		if c != colour {
			flush()
			colour = c
		}
		run += s
	}
	edge := "   +" + strings.Repeat("-", asciiColumns) + "+"
	write(edge, "")
	for r, row := range cells {
		write(fmt.Sprintf("\n%3d|", r*20), "")
		for _, c := range row {
			write(html.EscapeString(string(c.glyph)), c.colour)
		}
		write("|", "")
	}
	write("\n"+edge+"\n    0        100       200       300       400      500", "")
	flush()
	return template.HTML(b.String())
}

// Markers are what a paper map shows over the terrain.
type Markers struct {
	Small     bool               // the map on a player's page: no names, items or quest
	Highlight string             // the player to ring and name
	Show      func(*Player) bool // the players to show
}

// SVG draws the markers, for after the terrain in an <svg viewBox="0 0 500 500">.
func (m Markers) SVG(g *Game) template.HTML {
	var b strings.Builder
	q := g.Quest
	if !m.Small && q != nil && q.Type == 2 {
		fmt.Fprintf(&b, `<path stroke="%s" stroke-width="3" stroke-dasharray="2 7" stroke-linecap="round" d="M%d %d L%d %d"/>`,
			ink, q.P1[0], q.P1[1], q.P2[0], q.P2[1])
		for i, p := range []*[2]int{q.P1, q.P2} {
			class := "map-quest-point"
			if q.Stage > i+1 {
				class += " passed"
			}
			fmt.Fprintf(&b, `<g class="%s" transform="translate(%d %d)"><title>Точка квесту %d: [%d, %d]</title>`+
				`<path d="M0 -13 L3.8 -4.2 13 -4 5.8 2 8 11 0 6 -8 11 -5.8 2 -13 -4 -3.8 -4.2Z" stroke="%s" stroke-width="2" stroke-linejoin="round"/>`+
				`<text y="4" text-anchor="middle" font-family="Ubuntu Mono,monospace" font-weight="700" font-size="11" fill="%s">%d</text></g>`,
				class, p[0], p[1], i+1, p[0], p[1], ink, ink, i+1)
		}
	}
	if !m.Small {
		for _, it := range g.MapItems {
			fmt.Fprintf(&b, `<g transform="translate(%d %d)"><title>%s</title>`+
				`<rect x="-6" y="-6" width="12" height="12" transform="rotate(45)" fill="#ff6fae" stroke="%s" stroke-width="2"/></g>`,
				it.X, it.Y, html.EscapeString(fmt.Sprintf("%s, рівень %d [%d, %d]", slotName(it.Type), it.Level, it.X, it.Y)), ink)
		}
	}
	var questers []string
	if !m.Small && q != nil {
		questers = q.Players
	}
	for _, p := range g.Players {
		if m.Show != nil && !m.Show(p) {
			continue
		}
		b.WriteString(m.player(p, slices.Contains(questers, p.Name)))
	}
	return template.HTML(b.String())
}

func (m Markers) player(p *Player, questing bool) string {
	highlighted := p.Name == m.Highlight
	radius := 6.5
	switch {
	case highlighted:
		radius = 10
	case m.Small:
		radius = 5
	}
	kind := "offline"
	switch {
	case p.NPC:
		kind = "npc"
	case p.Online:
		kind = "online"
	}
	npc, status := "", "поза грою"
	if p.NPC {
		npc = " (NPC)"
	}
	if p.Online {
		status = "в грі"
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<g class="map-player %s" transform="translate(%d %d)"><title>%s</title>`, kind, p.X, p.Y,
		html.EscapeString(fmt.Sprintf("%s%s, рівень %d, %s [%d, %d]", p.Name, npc, p.Level, status, p.X, p.Y)))
	if highlighted {
		fmt.Fprintf(&b, `<circle class="map-player-halo" r="18" fill="#ffd43b" fill-opacity=".45" stroke="%s" stroke-width="2" stroke-dasharray="3 4"/>`, ink)
	}
	fmt.Fprintf(&b, `<circle class="map-player-dot" stroke="%s" stroke-width="2.5" r="%g"/>`, ink, radius)
	if questing {
		fmt.Fprintf(&b, `<circle class="map-player-quest" fill="none" stroke="#ff6fae" stroke-width="2.5" r="%g"/>`, radius+4)
	}
	if !m.Small || highlighted {
		size := 12
		if highlighted {
			size = 15
		}
		fmt.Fprintf(&b, `<text class="map-player-name" y="4" font-family="Fixel Text,system-ui,sans-serif" font-weight="800" fill="%s" stroke="#fff8e3" stroke-width="3.5" paint-order="stroke" x="%g" font-size="%d">%s</text>`,
			ink, radius+4, size, html.EscapeString(p.Name))
	}
	b.WriteString(`</g>`)
	// the small map is a link to the big one as a whole
	if m.Small {
		return b.String()
	}
	return fmt.Sprintf(`<a href="%s">%s</a>`, playerURL(p.Name), b.String())
}
