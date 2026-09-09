FROM golang:1.26.8-alpine3.24 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/exporter

FROM alpine:3.24

RUN apk --no-cache add ca-certificates \
     && addgroup -S exporter \
     && adduser -S -G exporter exporter
USER exporter
COPY --from=build /bin/exporter /bin/exporter

ENTRYPOINT ["/bin/exporter"]
