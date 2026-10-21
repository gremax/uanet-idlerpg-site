# uanet-idlerpg-site

The site of the IdleRPG on UAnet, rpg.uanet.net. The game bot is
[gremax/uanet-idlerpg](https://github.com/gremax/uanet-idlerpg).

The bot writes `public.json` every minute and right after a level up, with only
the public fields: no password hashes, IRC nicks or user@host. This program is
one Go binary with no dependencies outside the standard library. It reads that
file, again whenever its modification time or size changes, and renders every
page on each request, so the timers show the time left at that moment. If the
file goes missing or can't be parsed, it keeps showing the last good copy.

```
uanet-idlerpg-site -addr :8080 -data /data/public.json
```

- `-addr` (`:8080`) and `-data` (`/data/public.json`), the defaults the image uses.
- `-poll` (3 seconds): how often to check the file for changes.
- `-now`: a fixed time to render the game at, for screenshots.

Routes: `/`, `/players`, `/player/{name}`, `/map`, `/quest`, `/game.json` (the
file as read, 503 until there is one), `/healthz`, and `/static/`, where every
URL carries a hash of the file and is cached as immutable.

Checks, as CI runs them: `golangci-lint run` (set up in `.golangci.yml`),
`go test ./...`, and the [Nu HTML checker](https://validator.github.io/validator/)
on the golden pages (see below), `vnu --errors-only testdata/golden`. Without
the tools installed, Docker has both:

```
docker run --rm -v "$PWD:/app" -w /app golangci/golangci-lint:v2.15.0 golangci-lint run
docker run --rm -v "$PWD/testdata/golden:/pages:ro" ghcr.io/validator/validator vnu --errors-only /pages
```

## Pages

`templates/` holds one layout, a template per page, and in `partials/` the
pieces several pages share: the maps, the ASCII tables, names made links. Each
page has its whole markup twice: the uanet.net look and, behind «>_ Для олдів»,
an amber terminal that prints the same as command output. `<html
data-mode="term">` picks the terminal and the choice is kept in `localStorage`.
Every page is complete without JavaScript.

All the markup is in the templates. The Go code works out what they show: the
ranking, the padding of every ASCII table cell, where the trees stand.

`static/app.js` is the only script. It counts the timers down from their
`data-left` attributes, switches the look, copies the join commands and quietly
fetches the page again every minute. When a timer reaches zero it shows
`00:00:00` and asks again every 10 seconds until the bot writes the new level.
A file older than 5 minutes is stale: the pages say so, and the timers to the
next level stand at what the file says, since the bot isn't counting either.

The map's terrain is made up and laid out at startup from the fixed seed 42 in
`terrain.go`, so it is the same on every start. The four parts of the realm match
the bot's wars (split at x and y 250, small y is north), and the places sit at
the points of the quests in the bot's `events.uk.txt`. The names of the unique
items in `render.go` must match the bot's `lang/uk.txt`.

`testdata/` has a few games (both quest types, no quest, a stale file) and the
pages they render in `testdata/golden/`. After changing a page, look at the diff
of `go test -update` and commit the golden files with it. To look at the pages:

```
go run . -addr :8000 -data testdata/type2.json -now 2026-10-21T01:47:00Z
```

## Container

Every push to `main` publishes `ghcr.io/gremax/uanet-idlerpg-site:sha-<commit>`,
a distroless image that runs as nonroot with no arguments. It runs next to the
bot with the bot's volume mounted read only at `/data` and serves port 8080;
probe `/healthz`.

## License

MIT, see `LICENSE`. The code is new; it takes no code from the original IdleRPG
or its site. The one thing from the game is the names of its eight unique items,
translated, in `render.go`. The fonts in `static/fonts/` keep their own licenses
(see `static/fonts/README.txt`).
