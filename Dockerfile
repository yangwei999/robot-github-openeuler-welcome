FROM golang:1.16.3 as BUILDER

MAINTAINER zengchen1024<chenzeng765@gmail.com>

# build binary
WORKDIR /go/src/github.com/opensourceways/robot-github-openeuler-welcome
COPY . .
RUN GO111MODULE=on CGO_ENABLED=0 go build -a -o robot-github-openeuler-welcome .

# copy binary config and utils
FROM alpine:3.14
COPY  --from=BUILDER /go/src/github.com/opensourceways/robot-github-openeuler-welcome/robot-github-openeuler-welcome /opt/app/robot-github-openeuler-welcome

ENTRYPOINT ["/opt/app/robot-github-openeuler-welcome"]
