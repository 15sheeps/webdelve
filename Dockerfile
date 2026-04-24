# this image will build delve server and have mimimal footprint
# throwing away whole go toolchain

# minimal alpine image containing go toolchain
FROM golang:alpine AS builder

# install build-base for necessary compilation tools
RUN apk add --no-cache build-base

WORKDIR /build

COPY cmd/delve/server.go server.go

RUN go mod init main
RUN go mod tidy
# CGO_ENABLED=0 so binary won't depend on glibc
# ldflag -s disables the symbol table to reduce binary size
#        -w disables DWARF generation
#		 -extldflags '-static' makes static binary
RUN CGO_ENABLED=0 go build -o dlvsrv -ldflags "-s -w -extldflags '-static'" server.go

# -----------------------------------------------------------------------------------
FROM alpine:latest
COPY --from=builder /build/dlvsrv /sandbox/dlvsrv

# base work directory
WORKDIR /sandbox

# expose delve's default port
EXPOSE 2345

ENTRYPOINT ["/sandbox/dlvsrv"]
