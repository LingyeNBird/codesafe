#!/usr/bin/env bash
# Copyright (C) 2026 codesafe contributors
# SPDX-License-Identifier: AGPL-3.0-or-later
# See COPYING in the project root for the full license.
#
# codesafe 安装脚本：从 GitHub release 下载最新二进制，装入 ~/.local/bin，
# 并把 PATH 与别名写入 shell 配置文件。
#
# 用法: curl -fsSL https://raw.githubusercontent.com/LingyeNBird/codesafe/main/install.sh | bash

set -euo pipefail

REPO="LingyeNBird/codesafe"
INSTALL_DIR="${HOME}/.local/bin"
BIN="codesafe"

# 识别平台资产名。
detect_asset() {
    local os arch
    os="$(uname -s)"
    arch="$(uname -m)"
    case "$os" in
        Linux)  os="linux" ;;
        Darwin) os="darwin" ;;
        *)      echo "unsupported OS: $os" >&2; exit 1 ;;
    esac
    case "$arch" in
        x86_64|amd64) arch="amd64" ;;
        arm64|aarch64) arch="arm64" ;;
        *)            echo "unsupported arch: $arch" >&2; exit 1 ;;
    esac
    echo "codesafe-${os}-${arch}"
}

# 下载最新 release 的对应资产到安装目录。
install_binary() {
    local asset url tmp
    asset="$(detect_asset)"
    url="https://github.com/${REPO}/releases/latest/download/${asset}"
    mkdir -p "$INSTALL_DIR"
    tmp="$(mktemp)"
    echo "Downloading ${url}"
    curl -fsSL "$url" -o "$tmp"
    chmod +x "$tmp"
    mv -f "$tmp" "${INSTALL_DIR}/${BIN}"
    echo "Installed to ${INSTALL_DIR}/${BIN}"
}

# 往一个 rc 文件里写入 PATH 与别名（幂等：已存在则跳过）。
setup_rc() {
    local rc="$1"
    [ -f "$rc" ] || return 0
    if ! grep -q 'codesafe' "$rc"; then
        {
            echo ''
            echo '# codesafe'
            echo "export PATH=\"${INSTALL_DIR}:\$PATH\""
            echo "alias cs='codesafe'"
        } >> "$rc"
        echo "Updated ${rc}"
    fi
}

# 依据已有配置文件识别用户用的 shell；识别不了就只写 bashrc。
install_path_and_alias() {
    local wrote=0
    for rc in "${HOME}/.bashrc" "${HOME}/.zshrc"; do
        if [ -f "$rc" ]; then
            setup_rc "$rc"
            wrote=1
        fi
    done
    # fish
    if [ -d "${HOME}/.config/fish" ]; then
        local fishconf="${HOME}/.config/fish/config.fish"
        touch "$fishconf"
        if ! grep -q 'codesafe' "$fishconf"; then
            {
                echo ''
                echo '# codesafe'
                echo "fish_add_path ${INSTALL_DIR}"
                echo "alias cs codesafe"
            } >> "$fishconf"
            echo "Updated ${fishconf}"
        fi
        wrote=1
    fi
    if [ "$wrote" -eq 0 ]; then
        setup_rc "${HOME}/.bashrc"
    fi
}

main() {
    install_binary
    install_path_and_alias
    echo ""
    echo "Done. Restart your shell or run:  export PATH=\"${INSTALL_DIR}:\$PATH\""
    echo "Then run:  codesafe   (or alias: cs)"
    "${INSTALL_DIR}/${BIN}" --help >/dev/null 2>&1 || true
}

main "$@"
