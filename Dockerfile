# rpg.uanet.net: one static binary that serves the site on :8080 from the
# public.json the bot writes, read from the bot's volume mounted at /data.
FROM --platform=$BUILDPLATFORM golang:1.27.2 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY templates templates
COPY static static
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags=-s -o /site .

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /site /site
EXPOSE 8080
ENTRYPOINT ["/site"]
