FROM openeuler/openeuler:23.03 as BUILDER
RUN dnf update -y && \
    dnf install -y golang && \
    go env -w GOPROXY=https://goproxy.cn,direct

MAINTAINER zengchen1024<chenzeng765@gmail.com>

# build binary
WORKDIR /go/src/github.com/opensourceways/robot-github-openeuler-welcome
COPY . .
RUN GO111MODULE=on CGO_ENABLED=0 go build -a -o robot-github-openeuler-welcome .

# copy binary config and utils
FROM openeuler/openeuler:22.03
RUN dnf -y update && \
    dnf in -y shadow && \
    groupadd -g 1000 welcome && \
    useradd -u 1000 -g welcome -s /bin/bash -m welcome

COPY  --chown=welcome --from=BUILDER /go/src/github.com/opensourceways/robot-github-openeuler-welcome/robot-github-openeuler-welcome /opt/app/robot-github-openeuler-welcome

USER welcome

ENTRYPOINT ["/opt/app/robot-github-openeuler-welcome"]
