# syntax=docker/dockerfile:1
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mtga-metacrafter ./cmd/mtga-metacrafter

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/mtga-metacrafter /mtga-metacrafter
EXPOSE 8080
ENV LISTEN_ADDR=:8080 DATA_DIR=/data
USER nonroot:nonroot
ENTRYPOINT ["/mtga-metacrafter"]
