FROM golang:1.11-alpine3.8 as builder

RUN apk add --no-cache git

ENV GO111MODULE=on
ENV PACKAGE github.com/mopsalarm/kv
WORKDIR $GOPATH/src/$PACKAGE/

COPY go.mod go.sum ./
RUN go mod download

ENV CGO_ENABLED=0

COPY . .
RUN go build -v -ldflags="-s -w" -o /kv .


FROM alpine:3.8
RUN apk add --no-cache ca-certificates
EXPOSE 3080

COPY sql/ /sql/
COPY --from=builder /kv /

ENTRYPOINT ["/kv", "--verbose"]
