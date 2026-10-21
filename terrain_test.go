package main

import (
	"strings"
	"testing"
)

func TestF1(t *testing.T) {
	// toFixed(1) rounds an exact tie up; 0.05 and 2.45 are not ties as doubles
	for x, want := range map[float64]string{
		0: "0.0", 2.25: "2.3", 2.35: "2.4", 0.05: "0.1", 2.45: "2.5", 2.449: "2.4",
		123.04999: "123.0", 499.96: "500.0",
	} {
		if got := f1(x); got != want {
			t.Errorf("f1(%v) = %q, want %q", x, got, want)
		}
	}
}

func TestRandom(t *testing.T) {
	rand := random(42)
	// Park–Miller from 42: 42 × 16807 = 705894
	if got, want := rand(), float64(705894-1)/2147483646; got != want {
		t.Errorf("first number %v, want %v", got, want)
	}
}

func TestQuarterOf(t *testing.T) {
	for _, c := range []struct {
		x, y int
		want string
	}{
		{0, 0, "Північний Захід"}, {249, 249, "Північний Захід"}, {250, 249, "Північний Схід"},
		{249, 250, "Південний Захід"}, {250, 250, "Південний Схід"}, {500, 500, "Південний Схід"},
	} {
		if got := quarterOf(c.x, c.y); got != c.want {
			t.Errorf("quarterOf(%d, %d) = %q, want %q", c.x, c.y, got, c.want)
		}
	}
}

func TestPlaceAt(t *testing.T) {
	if got := placeAt(325, 270); got != "Столиця" {
		t.Errorf("the capital is %q", got)
	}
	if got := placeAt(225, 315); got != "Вежі Анкх-Аллора" {
		t.Errorf("a quest place is %q", got)
	}
	if got := placeAt(1, 1); got != "" {
		t.Errorf("nowhere is %q", got)
	}
}

func TestTerrainIsFixed(t *testing.T) {
	a, b := drawTerrain(42), drawTerrain(42)
	if a.Land != b.Land || a.grid != b.grid {
		t.Error("the same seed draws a different map")
	}
	if drawTerrain(43).Land == a.Land {
		t.Error("another seed draws the same map")
	}
	if !strings.Contains(string(a.Places), "<title>Столиця [325, 270]</title>") {
		t.Error("no capital on the map")
	}
}

func TestASCIIMarkers(t *testing.T) {
	g, err := parseGame([]byte(`{"updated": "2026-10-21T01:45:00Z",
		"players": [{"name": "a", "online": true, "x": 0, "y": 0}, {"name": "b", "x": 500, "y": 500},
		            {"name": "n", "npc": true, "x": 10, "y": 0}, {"name": "hidden", "online": true, "x": 20, "y": 0}],
		"mapitems": [{"x": 30, "y": 0, "type": "ring", "level": 1}],
		"quest": {"type": 2, "p1": [40, 0], "p2": [50, 0], "players": [], "stage": 1}}`))
	if err != nil {
		t.Fatal(err)
	}
	got := string(terrain.ASCII(g, func(p *Player) bool { return p.Name != "hidden" }))
	first := strings.Split(got, "\n")[1]
	want := `  0|<span class="t-hi">@</span><span class="t-gold">n</span>.<span class="t-bad">+</span><span class="t-gold">12</span>.`
	if !strings.HasPrefix(first, want) {
		t.Errorf("first row\n%s\nwant it to start\n%s", first, want)
	}
	last := strings.Split(got, "\n")[25]
	if !strings.HasSuffix(last, "o|") {
		t.Errorf("an offline player off the edge is not in the last cell: %s", last)
	}
}
