package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// now is the time the tests look at the variants in testdata at, two minutes
// after the bot wrote them.
var now = time.Date(2026, 10, 21, 1, 47, 0, 0, time.UTC)

func load(t *testing.T, variant string) *Game {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", variant+".json"))
	if err != nil {
		t.Fatal(err)
	}
	g, err := parseGame(data)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func names(players []*Player) []string {
	var n []string
	for _, p := range players {
		n = append(n, p.Name)
	}
	return n
}

func TestRanking(t *testing.T) {
	g := load(t, "type2")
	want := []string{"gremax", "ira", "Paganel", "nullbyte", "devnull", "tim"}
	if got := names(g.Humans); !slices.Equal(got, want) {
		t.Errorf("ranking %v, want %v", got, want)
	}
	if got := g.Rank(g.Find("Paganel")); got != 3 {
		t.Errorf("Paganel ranks %d, want 3", got)
	}
	if got := g.Rank(g.Find("Don")); got != 0 {
		t.Errorf("an NPC ranks %d, want 0", got)
	}
	if got := g.OnlineHumans(); got != 5 {
		t.Errorf("%d humans online, want 5", got)
	}
}

func TestRankingTies(t *testing.T) {
	g, err := parseGame([]byte(`{"updated": "2026-10-21T01:45:00Z", "players": [
		{"name": "b", "level": 5, "next": 100}, {"name": "a", "level": 5, "next": 100},
		{"name": "c", "level": 5, "next": 50}, {"name": "d", "level": 6, "next": 9000}]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"d", "c", "a", "b"}
	if got := names(g.Humans); !slices.Equal(got, want) {
		t.Errorf("ranking %v, want %v: by level, then time left, then name", got, want)
	}
}

func TestNPCs(t *testing.T) {
	g, err := parseGame([]byte(`{"updated": "2026-10-21T01:45:00Z", "npc": ["Don", "Ghost"],
		"players": [{"name": "Don", "npc": true}, {"name": "ira"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := names(g.NPCs); !slices.Equal(got, []string{"Don", "Ghost"}) {
		t.Errorf("NPCs %v, want Don and the stub Ghost", got)
	}
	if g.NPCs[0].Stub || !g.NPCs[1].Stub {
		t.Error("only an NPC without a character is a stub")
	}
	if got := names(g.Humans); !slices.Equal(got, []string{"ira"}) {
		t.Errorf("humans %v, want ira", got)
	}
}

func TestFind(t *testing.T) {
	g, err := parseGame([]byte(`{"updated": "2026-10-21T01:45:00Z",
		"players": [{"name": "Ira"}, {"name": "ira"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if p := g.Find("ira"); p != g.Players[1] {
		t.Error("an exact match comes first")
	}
	if p := g.Find("IRA"); p != g.Players[0] {
		t.Error("else the name is matched ignoring case")
	}
	if p := g.Find("nobody"); p != nil {
		t.Errorf("found %v for a name nobody has", p)
	}
}

func TestLeft(t *testing.T) {
	g := load(t, "type2")
	paganel := g.Find("Paganel") // next 523 at 01:45, online
	if got := g.Left(paganel, now); got != 403 {
		t.Errorf("Paganel has %d s left two minutes later, want 403", got)
	}
	if got := g.Left(paganel, now.Add(time.Hour)); got != 523 {
		t.Errorf("stale data: %d s left, want the 523 in the file", got)
	}
	if got := g.Left(&Player{Online: true, Next: 100}, now); got != 0 {
		t.Errorf("past the level: %d s left, want 0", got)
	}
	if got := g.Left(g.Find("nullbyte"), now); got != 15020 {
		t.Errorf("an offline timer stands: %d s left, want 15020", got)
	}
}

func TestStale(t *testing.T) {
	g := load(t, "type2")
	if g.Stale(now.Add(3 * time.Minute)) {
		t.Error("five minutes old is not stale yet")
	}
	if !g.Stale(now.Add(3*time.Minute + time.Second)) {
		t.Error("over five minutes old is stale")
	}
}

func TestDown(t *testing.T) {
	heard := now.Add(-time.Minute)
	g := &Game{Updated: now.Add(-time.Minute), Heard: &heard}
	if why := g.Down(now); why != "" {
		t.Errorf("a bot that writes and hears: %q, want none", why)
	}
	for _, c := range []struct {
		name string
		g    Game
		want string
	}{
		{"stale file", Game{Updated: now.Add(-6 * time.Minute), Heard: &heard}, "the bot has not written public.json for 6m0s"},
		{"disconnected", Game{Updated: now}, "the bot is not connected to IRC"},
		{"hears nothing", Game{Updated: now, Heard: ptr(now.Add(-53 * time.Minute))}, "the bot has heard nothing from IRC for 53m0s"},
	} {
		if why := c.g.Down(now); why != c.want {
			t.Errorf("%s: %q, want %q", c.name, why, c.want)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func TestParseRejects(t *testing.T) {
	for name, data := range map[string]string{
		"not JSON":         `{"updated": `,
		"no updated time":  `{"players": []}`,
		"type 2, no point": `{"updated": "2026-10-21T01:45:00Z", "quest": {"type": 2, "p1": [1, 2]}}`,
		"type 1, no end":   `{"updated": "2026-10-21T01:45:00Z", "quest": {"type": 1}}`,
		"another type":     `{"updated": "2026-10-21T01:45:00Z", "quest": {"type": 3}}`,
	} {
		if _, err := parseGame([]byte(data)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

func TestSource(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "public.json")
	write := func(data string, mtime time.Time) {
		t.Helper()
		if err := os.WriteFile(file, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(file, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	src := NewSource(file)
	src.Poll()
	if src.Game() != nil {
		t.Fatal("a game before the file exists")
	}

	write(`{"updated": "2026-10-21T01:45:00Z", "players": [{"name": "ira", "level": 1}]}`, now)
	src.Poll()
	if g := src.Game(); g == nil || g.Players[0].Level != 1 {
		t.Fatalf("got %v, want the game in the file", g)
	}

	write(`{"updated": "2026-10-21T01:46:00Z", "players": [{"name": "ira", "level": 2}]}`, now.Add(time.Minute))
	src.Poll()
	if g := src.Game(); g.Players[0].Level != 2 {
		t.Error("a changed file is read again")
	}

	write(`{"updated": "2026-10-2`, now.Add(2*time.Minute))
	src.Poll()
	if g := src.Game(); g == nil || g.Players[0].Level != 2 {
		t.Error("a broken file keeps the last good game")
	}

	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	src.Poll()
	if src.Game() == nil {
		t.Error("a missing file keeps the last good game")
	}
}
