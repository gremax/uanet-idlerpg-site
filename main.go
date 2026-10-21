// Command uanet-idlerpg-site serves rpg.uanet.net, the site of the IdleRPG on
// UAnet. It reads the public.json the bot writes, again whenever the file
// changes, and renders every page from the latest copy on each request.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	addr := flag.String("addr", ":8080", "address to serve HTTP on")
	data := flag.String("data", "/data/public.json", "the public.json the bot writes")
	poll := flag.Duration("poll", 3*time.Second, "how often to check public.json for changes")
	at := flag.String("now", "", "a fixed time to show the game at, like 2026-10-21T01:47:00Z, for screenshots")
	flag.Parse()

	now := time.Now
	if *at != "" {
		t, err := time.Parse(time.RFC3339, *at)
		if err != nil {
			slog.Error("bad -now", "err", err)
			os.Exit(2)
		}
		now = func() time.Time { return t }
	}

	site, err := NewSite()
	if err != nil {
		slog.Error("cannot load the templates", "err", err)
		os.Exit(1)
	}
	src := NewSource(*data)
	src.Poll()
	stop := make(chan struct{})
	go src.Watch(*poll, stop)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           site.Handler(src.Game, now),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-ctx.Done()
		close(stop)
		shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		srv.Shutdown(shutdown)
	}()
	slog.Info("serving", "addr", *addr, "data", *data)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		slog.Error("cannot serve", "err", err)
		os.Exit(1)
	}
}

// Handler serves the site from the game game() returns, at the time now()
// returns.
func (s *Site) Handler(game func() *Game, now func() time.Time) http.Handler {
	mux := http.NewServeMux()
	page := func(name string, data func(*Page, *http.Request) (any, bool)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			p := &Page{Game: game(), Now: now()}
			status := http.StatusOK
			if data != nil {
				var found bool
				if p.Data, found = data(p, r); !found {
					status = http.StatusNotFound
				}
			}
			s.serve(w, r, status, name, p)
		}
	}
	mux.HandleFunc("GET /{$}", page("index", indexData))
	mux.HandleFunc("GET /players", page("players", playersData))
	mux.HandleFunc("GET /map", page("map", mapData))
	mux.HandleFunc("GET /quest", page("quest", questData))
	mux.HandleFunc("GET /player/{name}", page("player", playerData))
	mux.HandleFunc("GET /player/{$}", page("player", playerData))
	mux.HandleFunc("GET /static/", func(w http.ResponseWriter, r *http.Request) {
		a, ok := s.assets[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", a.typ)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Write(a.body)
	})
	mux.HandleFunc("GET /game.json", func(w http.ResponseWriter, r *http.Request) {
		g := game()
		if g == nil {
			http.Error(w, "the game has not been read yet", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(g.JSON)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok\n"))
	})
	return mux
}

func (s *Site) serve(w http.ResponseWriter, r *http.Request, status int, name string, p *Page) {
	body, err := s.Render(name, p)
	if err != nil {
		slog.Error("cannot render", "page", name, "path", r.URL.Path, "err", err)
		http.Error(w, "the page failed to render", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(status)
	w.Write(body)
}
