FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY cluster-manager/go.mod cluster-manager/go.sum ./
RUN go mod download

COPY cluster-manager/*.go ./
RUN CGO_ENABLED=0 go build -o /cluster-manager .

FROM alpine:3.20

RUN apk add --no-cache bash coreutils

COPY --from=builder /cluster-manager /cluster-manager

EXPOSE 9090

ENTRYPOINT ["/cluster-manager"]
