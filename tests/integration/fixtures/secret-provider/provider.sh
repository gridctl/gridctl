#!/bin/sh
# Prints the marker file when it exists. Exits 1 otherwise.
# The path argument is accepted and ignored; the marker is the value source.
marker="${SECRET_PROVIDER_MARKER:-}"
if [ -z "$marker" ] || [ ! -f "$marker" ]; then
  exit 1
fi
cat "$marker"
