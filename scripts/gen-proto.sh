#!/bin/bash
set -e

# Regenerates the Go code for the enode.meta.v1 contract.
#
# The generated files are checked in, so nobody needs protoc to build this
# module — nor its importers: both crawlers and eNode-go. Run this only after
# editing a .proto.
#
# The plugins are installed at pinned versions into bin/tools rather than being
# taken from $PATH or added to go.mod: a floating plugin version rewrites the
# generated code with unrelated churn, and tool dependencies in go.mod leak into
# every consumer's module graph.

if [ ! -d "$PWD/scripts" ]; then
  echo "Please run this shell script from the repository's root folder."
  exit 1
fi

PROTOC_GEN_GO_VERSION="v1.36.11"
PROTOC_GEN_CONNECT_VERSION="v2.0.0-alpha.1"

MODULE_DIR="$PWD"
PROTO_DIR="$MODULE_DIR/proto"
OUT_DIR="$MODULE_DIR/gen"
TOOLS_DIR="$PWD/bin/tools"

command -v protoc >/dev/null 2>&1 || {
  echo "protoc is not installed. On macOS: brew install protobuf"
  exit 1
}

echo "Installing plugins into bin/tools..."
mkdir -p "$TOOLS_DIR"
GOBIN="$TOOLS_DIR" go install "google.golang.org/protobuf/cmd/protoc-gen-go@${PROTOC_GEN_GO_VERSION}"
GOBIN="$TOOLS_DIR" go install "connectrpc.com/connect/v2/cmd/protoc-gen-connect-go@${PROTOC_GEN_CONNECT_VERSION}"

echo "Generating..."
rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

# Run from the proto root so the import paths in the files ("enode/meta/v1/...")
# resolve, and so the generated packages land under gen/ in the same shape.
(
  cd "$PROTO_DIR"
  protoc \
    --plugin=protoc-gen-go="$TOOLS_DIR/protoc-gen-go" \
    --plugin=protoc-gen-connect-go="$TOOLS_DIR/protoc-gen-connect-go" \
    --go_out="$OUT_DIR" --go_opt=module=github.com/ModderMule/enodemeta/gen \
    --connect-go_out="$OUT_DIR" --connect-go_opt=module=github.com/ModderMule/enodemeta/gen \
    enode/meta/v1/meta.proto enode/meta/v1/ingest.proto
)

echo "Tidying the module..."
(cd "$MODULE_DIR" && go mod tidy)

echo "Done. Generated files:"
find "$OUT_DIR" -name '*.go' | sed "s|$PWD/||"
