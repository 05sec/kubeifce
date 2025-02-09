#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(dirname "${BASH_SOURCE[0]}")"
SCRIPT_ROOT="${SCRIPT_DIR}/.."
THIS_PKG="$(go list -m)"

# 获取 code-generator 路径（自动修复依赖）
get_codegen_pkg() {
  # 首次尝试获取路径
  CODEGEN_PKG=$(go list -m -f '{{.Dir}}' k8s.io/code-generator 2>/dev/null)

  # 如果未找到则尝试修复依赖
  if [[ -z "$CODEGEN_PKG" || ! -d "$CODEGEN_PKG" ]]; then
    echo "Could not find k8s.io/code-generator, running 'go mod tidy'..."
    go mod tidy -v  # 自动修复依赖
    CODEGEN_PKG=$(go list -m -f '{{.Dir}}' k8s.io/code-generator 2>/dev/null)
  fi

  # 最终检查
  if [[ -z "$CODEGEN_PKG" || ! -d "$CODEGEN_PKG" ]]; then
    echo "Error: k8s.io/code-generator still not found after 'go mod tidy'"
    exit 1
  fi

  echo "Using code-generator at: ${CODEGEN_PKG}"
}

# 获取 code-generator 路径
get_codegen_pkg

source "${CODEGEN_PKG}/kube_codegen.sh"

kube::codegen::gen_helpers \
    --boilerplate "${SCRIPT_ROOT}/hack/custom-boilerplate.go.txt" \
    "${SCRIPT_ROOT}"

kube::codegen::gen_client \
    --with-watch \
    --with-applyconfig \
    --output-dir "${SCRIPT_ROOT}/pkg" \
    --output-pkg "${THIS_PKG}/pkg" \
    --boilerplate "${SCRIPT_ROOT}/hack/boilerplate.go.txt" \
    "${SCRIPT_ROOT}/api"