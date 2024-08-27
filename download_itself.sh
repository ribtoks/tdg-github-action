#!/bin/bash
set -e
set -x

# Download the asset
curl -L "https://github.com/$GH_ACTION_REPOSITORY/releases/$INPUT_VERSION/download/$FILE" -o "$FILE"

head "$FILE"
