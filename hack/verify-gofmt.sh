#!/bin/bash

go_files=$(find . -name '*.go' -not -path './vendor/*' -not -path './deps/*' -print)
bad_files=$(gofmt -s -l ${go_files})
if [[ -n "${bad_files}" ]]; then
    (echo >&2 "!!! gofmt needs to be run on the listed files")
    echo "${bad_files}"
    (echo >&2 "Try running 'gofmt -s -d [path]' or autocorrect with 'hack/verify-gofmt.sh | xargs -n 1 gofmt -s -w'")
    exit 1
fi
