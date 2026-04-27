#!/bin/zsh

GOOS=windows GOARCH=amd64 go build -trimpath -o codetwin.exe ./cmd/codetwin/
go build -trimpath -o codetwin ./cmd/codetwin/

cp ./codetwin ~/.local/bin 2>&1