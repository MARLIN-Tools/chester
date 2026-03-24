#!/bin/bash
set -e

# load env vars
export $(grep -v '^#' .env | xargs)

# create build dir
mkdir -p build

# generate config.yml from template
envsubst < config.template.yml > build/config.yml

# build engine
GOOS=linux GOARCH=arm64 go build -o build/chester ./cmd/

# restart container
docker rm -f Chester || true

docker run -d \
  -v ${PWD}/build:/lichess-bot/config \
  --env OPTIONS=-v \
  --name Chester \
  lichessbotdevs/lichess-bot