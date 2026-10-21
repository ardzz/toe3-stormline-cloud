# syntax=docker/dockerfile:1

# The build stage runs on the build machine and cross-compiles, so multi-platform images need no emulation.
FROM --platform=$BUILDPLATFORM golang:1.27.3-trixie AS build
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /out/toe3-cloud ./cmd/toe3-cloud \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/toe3-cloud /usr/local/bin/toe3-cloud
# /data holds filesystem blobs; it is created owned by the nonroot user so a fresh named volume inherits that owner.
COPY --from=build --chown=nonroot:nonroot /out/data /data
ENV TOE3_LISTEN=0.0.0.0:8080 \
    TOE3_BLOB_DIR=/data/blobs
EXPOSE 8080
VOLUME /data
ENTRYPOINT ["/usr/local/bin/toe3-cloud"]
CMD ["serve"]
