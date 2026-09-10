FROM golang:1.26.8-alpine3.24 AS build

ARG VERSION=dev
ARG REVISION=unknown
ARG BRANCH=HEAD

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X github.com/prometheus/common/version.Version=${VERSION} -X github.com/prometheus/common/version.Revision=${REVISION} -X github.com/prometheus/common/version.Branch=${BRANCH}" \
    -o /bin/exporter

FROM alpine:3.24

RUN apk --no-cache add ca-certificates \
     && addgroup -S -g 65532 exporter \
     && adduser -S -u 65532 -G exporter exporter
USER 65532:65532
COPY --from=build /bin/exporter /bin/exporter

ENTRYPOINT ["/bin/exporter"]
