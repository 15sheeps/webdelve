# this image will build delve and have mimimal footprint
# throwing away whole go toolchain

# minimal alpine image containing go toolchain
FROM golang:alpine AS builder

# install build-base for necessary compilation tools
RUN apk add --no-cache build-base

# CGO_ENABLED=0 so binary won't depend on glibc
# ldflag -s disables the symbol table to reduce binary size
#        -w disables DWARF generation
#		 -extldflags '-static' makes static binary
RUN CGO_ENABLED=0 go install -ldflags "-s -w -extldflags '-static'" github.com/go-delve/delve/cmd/dlv@latest

FROM alpine:latest
COPY --from=builder /go/bin/dlv /usr/local/bin/dlv

# base work directory
WORKDIR /sandbox

RUN printf "module main\n" > go.mod

# expose delve's default port
EXPOSE 2345

# prevent container from exiting
CMD ["sleep", "infinity"]
