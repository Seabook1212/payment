#!/usr/bin/env bash

set -ev

SCRIPT_DIR=$(dirname "$0")

if [[ -z "$GROUP" ]] ; then
    echo "Cannot find GROUP env var"
    exit 1
fi

if [[ -z "$COMMIT" ]] ; then
    echo "Cannot find COMMIT env var"
    exit 1
fi

if [[ "$(uname)" == "Darwin" ]]; then
    DOCKER_CMD=docker
else
    DOCKER_CMD="sudo docker"
fi
CODE_DIR=$(cd $SCRIPT_DIR/..; pwd)
echo $CODE_DIR

REPO=${GROUP}/$(basename payment);

# Modern build using multi-stage Dockerfile directly from project root
# No need to copy files - Dockerfile handles everything
$DOCKER_CMD build -t ${REPO}:${COMMIT} -f $CODE_DIR/docker/payment/Dockerfile $CODE_DIR

# Also tag as latest for convenience
$DOCKER_CMD tag ${REPO}:${COMMIT} ${REPO}:latest

echo "Successfully built ${REPO}:${COMMIT}"
