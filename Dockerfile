FROM golang:1.23.2-bookworm AS builder

ARG ARCH=amd64 \
    VERSION=n/a \
    BUILD_DATE=n/a

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=${ARCH} \
    UPX_VER=4.2.4 \
    UPX_PKG=upx-${UPX_VER}-${ARCH}_linux.tar.xz

WORKDIR /build

RUN apt update && apt install xz-utils file -y
RUN wget https://github.com/upx/upx/releases/download/v${UPX_VER}/${UPX_PKG}}
RUN tar --lzma -xvf ${UPX_PKG} -C upx
RUN cp upx/upx /usr/local/bin

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .

RUN go build \
        -ldflags="-X 'main.Version=${VERSION}' -X 'main.BuildDate=${BUILD_DATE}'" \
        -o kube-ns-suspender \
        . \
    && file kube-ns-suspender \
    && strip kube-ns-suspender \
    && /usr/local/bin/upx -9 kube-ns-suspender


FROM gcr.io/distroless/base-debian12

WORKDIR /app

COPY --from=builder /build/kube-ns-suspender .

ENTRYPOINT [ "/app/kube-ns-suspender" ]
