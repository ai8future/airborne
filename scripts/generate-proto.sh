#!/bin/bash
set -euo pipefail

# Generate Go code from proto files using buf

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_ROOT"

echo "Generating protobuf code..."

# Clean existing generated code
rm -rf gen/go

# Generate code using buf
buf generate

echo "Proto code generation complete!"
echo "Generated files:"
find gen/go -name "*.go" 2>/dev/null || echo "  (none - check for errors)"
