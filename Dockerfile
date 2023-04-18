FROM golang:1.20.3-alpine3.17 as build
ENV GO111MODULE=on
ENV CGO_ENABLED=0
ENV GOOS=linux

RUN apk add --no-cache make git

WORKDIR /go/src/github.com/netlify/gotrue

# Pulling dependencies
COPY ./Makefile ./go.* ./
RUN make deps

# Building stuff
COPY . /go/src/github.com/netlify/gotrue
RUN make build

FROM alpine:3.17.3
RUN adduser -D -u 1000 netlify

RUN apk add --no-cache ca-certificates
COPY --from=build /go/src/github.com/netlify/gotrue/gotrue /usr/local/bin/gotrue

COPY hack/jwt.test.key /home/netlify/keys/jwt.test.key
COPY hack/jwt.test.key.pub /home/netlify/keys/jwt.test.key.pub

COPY hack/create_user.sh /home/netlify/create_user.sh
RUN ["chmod", "+x", "/home/netlify/create_user.sh"]

USER netlify
CMD ["/bin/sh","-c", "/home/netlify/create_user.sh && gotrue"]
