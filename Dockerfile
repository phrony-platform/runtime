FROM golang:1.25-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /out/phrony-runtime ./cmd/phrony-runtime

FROM gcr.io/distroless/static-debian12

COPY --from=build /out/phrony-runtime /usr/local/bin/phrony-runtime

EXPOSE 7777

ENTRYPOINT ["phrony-runtime"]
CMD ["serve"]
