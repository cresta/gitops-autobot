#!/bin/bash
go get -u -d
go mod tidy
git add go.mod go.sum
