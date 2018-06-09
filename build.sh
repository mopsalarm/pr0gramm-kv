#!/bin/sh
set -e

docker build -t mopsalarm/kv .
docker push mopsalarm/kv
