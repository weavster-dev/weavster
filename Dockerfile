# Multi-stage build: golang -> distroless non-root (single static binary).
FROM golang:1.22 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
# VERSION and BUILD_DATE stamp the binary (weavster version, and the release
# the database records for each schema upgrade): pass the release's, for
# example --build-arg VERSION=1.2.0 --build-arg BUILD_DATE=$(date -u +%FT%TZ).
ARG VERSION
ARG BUILD_DATE
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
    -ldflags="-s -w ${VERSION:+-X main.version=$VERSION} ${BUILD_DATE:+-X main.buildDate=$BUILD_DATE}" \
    -o /out/weavster ./cmd/weavster

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/weavster /weavster
COPY agent-docs/ /agent-docs/
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/weavster"]
CMD ["server", "0.0.0.0:8080"]
