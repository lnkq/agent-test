# Build both binaries (gateway + demo upstream) in one stage, run in a minimal image.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 go build -o /out/gateway ./cmd/server \
 && CGO_ENABLED=0 go build -o /out/upstream ./cmd/upstream

FROM alpine:3.20
COPY --from=build /out/gateway /out/upstream /usr/local/bin/
EXPOSE 8080
# Default binary for `docker run`; compose overrides this per service (gateway
# or upstream). Using CMD (not ENTRYPOINT) lets the command fully control argv.
CMD ["gateway"]
