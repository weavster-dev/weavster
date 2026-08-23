# Multi-stage build: golang -> distroless non-root (single static binary).
FROM golang:1.22 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o /out/weavster ./cmd/weavster

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/weavster /weavster
COPY agent-docs/ /agent-docs/
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/weavster"]
CMD ["server", "0.0.0.0:8080"]
