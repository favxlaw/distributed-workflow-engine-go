#!/usr/bin/env bash

set -euo pipefail

docker run --rm -p 8000:8000 amazon/dynamodb-local
