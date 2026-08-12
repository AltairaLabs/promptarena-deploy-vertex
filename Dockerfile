# syntax=docker/dockerfile:1

FROM golang:1.26 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN GOWORK=off go mod download

COPY cmd/ ./cmd/

ARG VERSION=dev
ENV GOWORK=off CGO_ENABLED=0 GOOS=linux

RUN go build -ldflags="-s -w -X main.Version=${VERSION}" \
    -o /out/vertex-runtime ./cmd/vertex-runtime/

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/vertex-runtime /vertex-runtime

# Agent Runtime requires the container to listen on 0.0.0.0:8080.
EXPOSE 8080

USER nonroot:nonroot
ENTRYPOINT ["/vertex-runtime"]
