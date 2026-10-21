package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// staleAfter is how old public.json may get before the site says the bot is
// not writing it. The bot rewrites it every minute.
const staleAfter = 5 * time.Minute

// Game is public.json as the bot writes it (lib/IRPG/Public.pm in the bot),
// plus what the pages derive from it.
type Game struct {
	Updated  time.Time `json:"updated"`
	Players  []*Player `json:"players"`
	NPCNames []string  `json:"npc"`
	MapItems []MapItem `json:"mapitems"`
	Quest    *Quest    `json:"quest"`
	Events   []Event   `json:"events"`

	// Humans is the ranking: by level, then by the least time to the next one.
	Humans []*Player `json:"-"`
	// NPCs are the players marked as NPCs, then the names in NPCNames the
	// bot has no character for, as stubs.
	NPCs []*Player `json:"-"`
	// JSON is the file as read, served as /game.json.
	JSON []byte `json:"-"`
}

type Player struct {
	Name      string            `json:"name"`
	Class     string            `json:"class"`
	Level     int               `json:"level"`
	Next      int64             `json:"next"` // seconds to the next level at Updated
	Idled     int64             `json:"idled"`
	Alignment string            `json:"alignment"` // g, n or e
	Online    bool              `json:"online"`
	NPC       bool              `json:"npc"`
	X         int               `json:"x"`
	Y         int               `json:"y"`
	Created   time.Time         `json:"created"`
	Items     map[string]int    `json:"items"`     // slot: level
	Unique    map[string]string `json:"unique"`    // slot: letter of a unique item
	Penalties map[string]int64  `json:"penalties"` // kind: seconds

	// Stub is an NPC from the npc list that has no character.
	Stub bool `json:"-"`
}

type MapItem struct {
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Type   string `json:"type"`
	Level  int    `json:"level"`
	Unique string `json:"unique"`
}

// Quest is the running quest. Type 1 lasts until Ends; type 2 is done when
// all four players reach P1, then P2, and Stage says which they walk to.
type Quest struct {
	Type    int        `json:"type"`
	Text    string     `json:"text"`
	Players []string   `json:"players"`
	P1      *[2]int    `json:"p1"`
	P2      *[2]int    `json:"p2"`
	Stage   int        `json:"stage"`
	Ends    *time.Time `json:"ends"`
}

type Event struct {
	Time time.Time `json:"t"`
	Text string    `json:"text"`
}

func parseGame(data []byte) (*Game, error) {
	var g Game
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&g); err != nil {
		return nil, err
	}
	if g.Updated.IsZero() {
		return nil, errors.New("no updated time")
	}
	if q := g.Quest; q != nil {
		switch {
		case q.Type == 1 && q.Ends == nil:
			return nil, errors.New("a type 1 quest without its end")
		case q.Type == 2 && (q.P1 == nil || q.P2 == nil):
			return nil, errors.New("a type 2 quest without its points")
		case q.Type != 1 && q.Type != 2:
			return nil, fmt.Errorf("a quest of type %d", q.Type)
		}
	}
	g.JSON = data
	for _, p := range g.Players {
		if p.NPC {
			g.NPCs = append(g.NPCs, p)
		} else {
			g.Humans = append(g.Humans, p)
		}
	}
	slices.SortStableFunc(g.Humans, func(a, b *Player) int {
		if a.Level != b.Level {
			return b.Level - a.Level
		}
		if a.Next != b.Next {
			return int(a.Next - b.Next)
		}
		return strings.Compare(a.Name, b.Name)
	})
	for _, name := range g.NPCNames {
		if !slices.ContainsFunc(g.NPCs, func(p *Player) bool { return p.Name == name }) {
			g.NPCs = append(g.NPCs, &Player{Name: name, NPC: true, Stub: true})
		}
	}
	return &g, nil
}

// Find returns the player called name, ignoring case if no name matches
// exactly, or nil.
func (g *Game) Find(name string) *Player {
	for _, p := range g.Players {
		if p.Name == name {
			return p
		}
	}
	for _, p := range g.Players {
		if strings.EqualFold(p.Name, name) {
			return p
		}
	}
	return nil
}

// Rank is the player's place in the ranking, or 0 for an NPC.
func (g *Game) Rank(p *Player) int {
	return slices.Index(g.Humans, p) + 1
}

func (g *Game) OnlineHumans() int {
	n := 0
	for _, p := range g.Humans {
		if p.Online {
			n++
		}
	}
	return n
}

// Stale says whether at now the bot has not written the file for too long.
func (g *Game) Stale(now time.Time) bool {
	return now.Sub(g.Updated) > staleAfter
}

// Left is the seconds to the player's next level at now. The bot counts
// down only while a player is online. Stale data would put every timer at
// zero, so then the time is the one in the file, and the pages say so.
func (g *Game) Left(p *Player, now time.Time) int64 {
	if !p.Online || g.Stale(now) {
		return p.Next
	}
	return max(0, p.Next-int64(now.Sub(g.Updated)/time.Second))
}

// Source keeps the latest good public.json and reads it again when it
// changes. A file that can't be read or parsed leaves the last good one.
type Source struct {
	path string

	mu    sync.RWMutex
	game  *Game
	stamp string // modification time and size of the file read last
}

func NewSource(path string) *Source {
	return &Source{path: path}
}

func (s *Source) Game() *Game {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.game
}

// Poll reads the file if it changed since the last call.
func (s *Source) Poll() {
	fi, err := os.Stat(s.path)
	if err != nil {
		s.fail("", err)
		return
	}
	stamp := fmt.Sprint(fi.ModTime().UnixNano(), fi.Size())
	s.mu.RLock()
	same := stamp == s.stamp
	s.mu.RUnlock()
	if same {
		return
	}
	data, err := os.ReadFile(s.path)
	if err == nil {
		var g *Game
		if g, err = parseGame(data); err == nil {
			s.mu.Lock()
			s.game, s.stamp = g, stamp
			s.mu.Unlock()
			return
		}
	}
	s.fail(stamp, err)
}

// fail logs a failed read once per version of the file, so a broken file
// is not retried, and a missing one is not logged every poll.
func (s *Source) fail(stamp string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stamp == "" {
		stamp = "missing"
	}
	if stamp != s.stamp {
		slog.Warn("cannot read the game, keeping the last one", "file", s.path, "err", err)
		s.stamp = stamp
	}
}

// Watch polls the file every interval until stop is closed.
func (s *Source) Watch(interval time.Duration, stop <-chan struct{}) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			s.Poll()
		}
	}
}
