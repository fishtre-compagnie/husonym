#!/bin/sh

FILE=$(readlink -f "$0")
HUSONYM_ROOT="$(dirname "$FILE")/../../"

cluster_create()
{
  if ! command -v ctlptl > /dev/null ; then
    echo "requires ctptl to run, see https://github.com/tilt-dev/ctlptl"
    exit 1
  fi


  HUSONYM_DEV_HOSTPATH="${HUSONYM_ROOT}/.data"
  mkdir -p "$HUSONYM_DEV_HOSTPATH"
  chmod 777 "$HUSONYM_DEV_HOSTPATH"
  sed 's|{HUSONYM_DEV_HOSTPATH}|'"$HUSONYM_DEV_HOSTPATH"'|' < "$HUSONYM_ROOT/tilt/kind/cluster.yaml" | ctlptl apply -f -
}

cluster_create
