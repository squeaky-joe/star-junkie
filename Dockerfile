# syntax=docker/dockerfile:1
ARG GO_VERSION=1.27
FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Pure-Go SQLite driver, so no CGO and a static binary.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bot ./cmd/bot

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/bot /bot
ENV DB_PATH=/data/bot.db
VOLUME /data
ENTRYPOINT ["/bot"]
