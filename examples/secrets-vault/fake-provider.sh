#!/bin/sh
# Example exec provider. Prints a stand-in value for the requested path.
# Do not use this script with a real credential.
printf 'example-value-for-%s\n' "$1"
