#!/bin/bash
set -euo pipefail

version=$(git describe)
go test
go install -v -race -ldflags "-X main.version=$version"

[[ ${1:-} == "pkg" ]] || exit 0

build() {
    name="imapchive-$version-$1-$2"
    rm -rf "build/$name"
    mkdir -p "build/$name"
    GOOS=$1 GOARCH=$2 go build -i -v -ldflags "-s -w -X main.version=$version" -o "build/$name/imapchive$3"
}

rm -rf build
for os in darwin freebsd ; do
    build $os amd64 ""
done
for arch in amd64 386 ; do
    build windows $arch ".exe"
done
for arch in amd64 386 arm64 arm mips mipsle mips64 mips64le ; do
    build linux $arch ""
done
