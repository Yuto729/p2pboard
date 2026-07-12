# build stage
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/p2pboard ./cmd/p2pboard

# runtime stage
FROM alpine:3.20
RUN adduser -D -u 1000 p2p
COPY --from=build /out/p2pboard /usr/local/bin/p2pboard
USER p2p
ENTRYPOINT ["p2pboard"]
