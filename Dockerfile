# syntax=docker/dockerfile:1
ARG GO_VERSION=1.27
FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Pure-Go SQLite driver, so no CGO and a static binary.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bot ./cmd/bot
# distroless has no shell, so prepare the data dir here and copy it in with the right owner.
RUN mkdir /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/bot /bot
COPY --chown=nonroot:nonroot content /content
COPY --from=build --chown=nonroot:nonroot /out/data /data
ENV DB_PATH=/data/bot.db CONTENT_DIR=/content
VOLUME /data
ENTRYPOINT ["/bot"]
