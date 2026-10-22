#!/bin/sh
# Runs an action from hostdeck's detail page: "open <mount point>".
set -eu
case "$1" in
open) open "$2" ;;
*) echo "unknown action: $1" >&2; exit 2 ;;
esac
