package main

import (
	"fmt"
	"math"
	"slices"
	"strconv"
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

type circle struct{ X, Y, R float64 }

func (c circle) has(x, y float64) bool {
	return (x-c.X)*(x-c.X)+(y-c.Y)*(y-c.Y) <= c.R*c.R
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
	Text   string
	X, Y   int
	Anchor string
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
	distance := math.Sqrt(rand()) * c.R
	return c.X + math.Cos(angle)*distance, c.Y + math.Sin(angle)*distance
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

// f1 is x with one decimal, enough for a 500 × 500 map.
func f1(x float64) string {
	return strconv.FormatFloat(x, 'f', 1, 64)
}

// hatch is hatching across a circle as plain strokes, 7 apart at 45°.
func hatch(c circle) string {
	const step = 7.0
	offset := (c.X+c.Y)/math.Sqrt2 - 0.5
	var b strings.Builder
	for k := math.Ceil((offset - c.R) / step); k*step < offset+c.R; k++ {
		d := offset - k*step
		half := math.Sqrt(c.R*c.R-d*d) - 1
		if half <= 0 {
			continue
		}
		fx, fy := c.X-d/math.Sqrt2, c.Y-d/math.Sqrt2
		ex := half / math.Sqrt2
		fmt.Fprintf(&b, "M%s %sL%s %s", f1(fx+ex), f1(fy-ex), f1(fx-ex), f1(fy+ex))
	}
	return b.String()
}

// Terrain is the map's land, laid out once: what the "terrain" template draws
// on a paper map, and the grid of glyphs for the terminal's.
type Terrain struct {
	GridLines string // path data: the faint lines every 50
	DarkLands circle
	Hatching  string // path data, across the dark lands
	Reeds     string // path data, in the swamp
	River     string // path data
	Lake      circle
	Trees     []circle
	Hills     []string // path data, one per hill
	Mountains []mountain
	Places    []Place
	Capital   Place
	// the names of the four parts, of the land, and of the quest places
	QuarterNames, LandNames, PlaceNames []mapText

	grid [asciiRows][asciiColumns]rune
}

// mountain is path data: a peak and its snowcap.
type mountain struct{ Peak, Snow string }

var terrain = layTerrain(42)

// layTerrain lays the land out, with the trees, hills and mountains placed at
// random from seed.
func layTerrain(seed int64) *Terrain {
	rand := random(seed)
	t := &Terrain{DarkLands: darkLands, Hatching: hatch(darkLands), Lake: lake, Places: places,
		Capital: capital, QuarterNames: quarterNames, LandNames: landNames, grid: drawGrid()}

	var lines strings.Builder
	for g := 50; g < worldSize; g += 50 {
		if g != 250 {
			fmt.Fprintf(&lines, "M%d 0V%dM0 %dH%d", g, worldSize, g, worldSize)
		}
	}
	t.GridLines = lines.String()

	var reeds strings.Builder
	for range 60 {
		x, y := pointIn(rand, swamp)
		fmt.Fprintf(&reeds, "M%s %sh8", f1(x-4), f1(y))
	}
	t.Reeds = reeds.String()

	var points []string
	for _, p := range river {
		points = append(points, fmt.Sprintf("%g %g", p[0], p[1]))
	}
	t.River = "M" + strings.Join(points, " L")

	for _, f := range forests {
		for range int(math.Floor(f.R*0.9 + 0.5)) {
			x, y := pointIn(rand, f)
			if riverDistance(x, y) < 10 {
				continue
			}
			t.Trees = append(t.Trees, circle{x, y, 5 + rand()*3})
		}
	}

	for range 9 {
		x, y := pointIn(rand, circle{hills.X, hills.Y, hills.R - 6})
		t.Hills = append(t.Hills, fmt.Sprintf("M%s %s q9 -12 18 0", f1(x-9), f1(y)))
	}

	for range 14 {
		x, base := pointIn(rand, circle{mountains.X, mountains.Y, mountains.R - 8})
		y := base + 8
		h := 14 + rand()*10
		t.Mountains = append(t.Mountains, mountain{
			Peak: fmt.Sprintf("M%s %s L%s %s L%s %sZ", f1(x-h*0.7), f1(y), f1(x), f1(y-h), f1(x+h*0.7), f1(y)),
			Snow: fmt.Sprintf("M%s %s L%s %s L%s %s", f1(x-h*0.22), f1(y-h*0.68), f1(x), f1(y-h), f1(x+h*0.22), f1(y-h*0.68)),
		})
	}

	for _, p := range places {
		if o, ok := labelOffset[p.Label]; ok {
			t.PlaceNames = append(t.PlaceNames, mapText{p.Name, p.X + o.dx, p.Y + o.dy, o.anchor})
		}
	}
	return t
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

// Run is a stretch of the terminal map in one colour, or in the colour of
// the <pre> around it.
type Run struct{ Text, Class string }

// ASCII lays out the terminal map with the game's markers: items, the quest's
// points, and the players for whom show is true.
func (t *Terrain) ASCII(g *Game, show func(*Player) bool) []Run {
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

	var runs []Run
	write := func(text, colour string) {
		if n := len(runs); n > 0 && runs[n-1].Class == colour {
			runs[n-1].Text += text
			return
		}
		runs = append(runs, Run{text, colour})
	}
	edge := "   +" + strings.Repeat("-", asciiColumns) + "+"
	write(edge, "")
	for r, row := range cells {
		write(fmt.Sprintf("\n%3d|", r*20), "")
		for _, c := range row {
			write(string(c.glyph), c.colour)
		}
		write("|", "")
	}
	write("\n"+edge+"\n    0        100       200       300       400      500", "")
	return runs
}

// Markers say what a paper map shows over the terrain.
type Markers struct {
	Small     bool               // the map on a player's page: no names, items or quest
	Highlight string             // the player to ring and name
	Show      func(*Player) bool // the players to show
}

// Overlay is what the "markers" template draws after the terrain in an
// <svg viewBox="0 0 500 500">.
type Overlay struct {
	Route   []QuestPoint // the two points of a quest to walk
	Items   []MapItem
	Players []Mark
}

type QuestPoint struct {
	N, X, Y int
	Passed  bool
}

// Mark is a player on a paper map.
type Mark struct {
	*Player
	Radius      float64
	Highlighted bool // ringed and named, also on the small map
	Questing    bool // ringed as on the quest
	Named       bool
	Linked      bool // a link to the player's page
}

// For is the overlay of the game.
func (m Markers) For(g *Game) Overlay {
	var o Overlay
	var questers []string
	if q := g.Quest; !m.Small && q != nil {
		questers = q.Players
		if q.Type == 2 {
			o.Route = []QuestPoint{
				{1, q.P1[0], q.P1[1], q.Stage > 1},
				{2, q.P2[0], q.P2[1], q.Stage > 2},
			}
		}
	}
	if !m.Small {
		o.Items = g.MapItems
	}
	for _, p := range g.Players {
		if m.Show != nil && !m.Show(p) {
			continue
		}
		mark := Mark{Player: p, Radius: 6.5, Highlighted: p.Name == m.Highlight,
			Questing: slices.Contains(questers, p.Name), Named: !m.Small, Linked: !m.Small}
		switch {
		case mark.Highlighted:
			mark.Radius, mark.Named = 10, true
		case m.Small:
			mark.Radius = 5
		}
		o.Players = append(o.Players, mark)
	}
	return o
}

// Kind is the class of the mark: npc, online or offline.
func (m Mark) Kind() string {
	switch {
	case m.NPC:
		return "npc"
	case m.Online:
		return "online"
	}
	return "offline"
}

// Ring is the radius of the quest ring, and where the name starts.
func (m Mark) Ring() float64 { return m.Radius + 4 }

func (m Mark) FontSize() int {
	if m.Highlighted {
		return 15
	}
	return 12
}
