FROM golang:1.26.4-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /out/kano-go ./cmd/kano-go

FROM scratch
COPY --from=build /out/kano-go /kano-go
EXPOSE 7658
ENTRYPOINT ["/kano-go"]
