FROM node:16 as builder

WORKDIR /web
COPY ./VERSION .
COPY ./web .

#WORKDIR /web/default
#RUN npm install --registry https://registry.npmmirror.com
#RUN DISABLE_ESLINT_PLUGIN='true' REACT_APP_VERSION=$(cat ../VERSION) npm run build

WORKDIR /web/berry
RUN npm install --registry https://registry.npmmirror.com
RUN DISABLE_ESLINT_PLUGIN='true' REACT_APP_VERSION=$(cat ../VERSION) npm run build

FROM golang AS builder2

ENV GO111MODULE=on \
    CGO_ENABLED=1 \
    GOOS=linux \
    GOPROXY=https://goproxy.cn,direct

WORKDIR /build
ADD go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=builder /web/build ./web/build
RUN go build -ldflags "-s -w -X 'one-api/common.Version=$(cat VERSION)' -extldflags '-static'" -o one-api

FROM alpine

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories
RUN apk update \
    && apk upgrade \
    && apk add --no-cache ca-certificates tzdata \
    && update-ca-certificates 2>/dev/null || true

COPY --from=builder2 /build/one-api /
EXPOSE 3000
WORKDIR /data
ENTRYPOINT ["/one-api"]