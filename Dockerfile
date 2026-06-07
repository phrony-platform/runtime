FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS build

ARG TARGETARCH
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -o /out/phrony-runtime ./cmd/phrony-runtime

FROM --platform=$TARGETPLATFORM gcr.io/distroless/static-debian12

COPY --from=build /out/phrony-runtime /usr/local/bin/phrony-runtime

EXPOSE 7777

ENTRYPOINT ["phrony-runtime"]
CMD ["serve"]
