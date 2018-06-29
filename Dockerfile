FROM golang:1.11beta1-alpine as builder

WORKDIR $GOPATH/src/github.com/mopsalarm/kv

ADD https://github.com/golang/dep/releases/download/v0.4.1/dep-linux-amd64 /usr/bin/dep
RUN chmod a+x /usr/bin/dep

RUN apk add --no-cache git ca-certificates

COPY Gopkg.toml Gopkg.lock ./
RUN dep ensure --vendor-only -v

COPY . .
RUN go build -v -ldflags="-s -w" -o /binary

FROM alpine:3.7
EXPOSE 3080

RUN apk add --no-cache ca-certificates

COPY sql/ /sql/
COPY --from=builder /binary /kv

ENTRYPOINT ["/kv", "--verbose"]
