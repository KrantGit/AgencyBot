FROM golang:1.25.3-alpine3.22 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/user-service ./cmd/user-service && CGO_ENABLED=0 go build -o /out/stub ./cmd/stub

FROM alpine:3.22
RUN adduser -D -H app
USER app
COPY --from=build /out /app
ENTRYPOINT ["/app/user-service"]
