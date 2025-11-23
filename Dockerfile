FROM golang:1.24.1-alpine3.21 AS build-stage

WORKDIR /app

COPY go.mod go.sum ./

RUN --mount=type=cache,target=/root/go-build go mod download -x

COPY . .

ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64

RUN go test -v ./... && go build -ldflags="-s -w" -o ./pgrwl

FROM alpine:3.21.5 AS build-release-stage

RUN apk update && apk add --no-cache \
    bash \
    vim \
    mc

COPY --from=build-stage /app/pgrwl /usr/local/bin/pgrwl

RUN chmod +x /usr/local/bin/pgrwl

ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64

ENTRYPOINT ["/usr/local/bin/pgrwl"]
