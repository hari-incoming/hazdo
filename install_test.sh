#!/bin/sh
# install_test.sh - Tests for install.sh
#
# Validates the install script functions work correctly.
# Run: sh install_test.sh
#
# These tests source install.sh and verify individual functions
# without actually downloading or installing anything.

set -e

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# ─── Test helpers ──────────────────────────────────────────────────────────────

assert_eq() {
    TESTS_RUN=$((TESTS_RUN + 1))
    expected="$1"
    actual="$2"
    test_name="$3"

    if [ "$expected" = "$actual" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        printf "  \033[0;32mPASS\033[0m %s\n" "$test_name"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        printf "  \033[0;31mFAIL\033[0m %s\n" "$test_name"
        printf "       Expected: '%s'\n" "$expected"
        printf "       Got:      '%s'\n" "$actual"
    fi
}

assert_not_empty() {
    TESTS_RUN=$((TESTS_RUN + 1))
    value="$1"
    test_name="$2"

    if [ -n "$value" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        printf "  \033[0;32mPASS\033[0m %s\n" "$test_name"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        printf "  \033[0;31mFAIL\033[0m %s\n" "$test_name"
        printf "       Expected non-empty value\n"
    fi
}

assert_file_exists() {
    TESTS_RUN=$((TESTS_RUN + 1))
    filepath="$1"
    test_name="$2"

    if [ -f "$filepath" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        printf "  \033[0;32mPASS\033[0m %s\n" "$test_name"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        printf "  \033[0;31mFAIL\033[0m %s\n" "$test_name"
        printf "       File not found: %s\n" "$filepath"
    fi
}

assert_contains() {
    TESTS_RUN=$((TESTS_RUN + 1))
    haystack="$1"
    needle="$2"
    test_name="$3"

    if printf '%s' "$haystack" | grep -q "$needle"; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        printf "  \033[0;32mPASS\033[0m %s\n" "$test_name"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        printf "  \033[0;31mFAIL\033[0m %s\n" "$test_name"
        printf "       '%s' not found in output\n" "$needle"
    fi
}

# ─── Test: OS detection ───────────────────────────────────────────────────────

test_detect_os() {
    printf "\n\033[1mTest: OS detection\033[0m\n"

    # Source the script functions
    OS_NAME=""
    detect_os >/dev/null 2>&1

    current_os="$(uname -s)"
    case "$current_os" in
        Linux*)   expected="Linux" ;;
        Darwin*)  expected="Darwin" ;;
        MINGW*|MSYS*|CYGWIN*) expected="Windows" ;;
        *)        expected="unknown" ;;
    esac

    assert_eq "$expected" "$OS_NAME" "OS detection matches uname"
    assert_not_empty "$OS_NAME" "OS_NAME is set"
}

# ─── Test: Architecture detection ─────────────────────────────────────────────

test_detect_arch() {
    printf "\n\033[1mTest: Architecture detection\033[0m\n"

    ARCH_NAME=""
    detect_arch >/dev/null 2>&1

    assert_not_empty "$ARCH_NAME" "ARCH_NAME is set"

    # Should be one of the supported architectures
    case "$ARCH_NAME" in
        x86_64|arm64)
            assert_eq "1" "1" "ARCH_NAME is a supported value ($ARCH_NAME)"
            ;;
        *)
            assert_eq "x86_64 or arm64" "$ARCH_NAME" "ARCH_NAME is a supported value"
            ;;
    esac
}

# ─── Test: Dependency checks ──────────────────────────────────────────────────

test_check_dependencies() {
    printf "\n\033[1mTest: Dependency checks\033[0m\n"

    OS_NAME="Linux"
    DOWNLOADER=""
    check_dependencies >/dev/null 2>&1

    assert_not_empty "$DOWNLOADER" "DOWNLOADER is set"

    case "$DOWNLOADER" in
        curl|wget)
            assert_eq "1" "1" "DOWNLOADER is curl or wget ($DOWNLOADER)"
            ;;
        *)
            assert_eq "curl or wget" "$DOWNLOADER" "DOWNLOADER is valid"
            ;;
    esac
}

# ─── Test: Linux distribution detection ───────────────────────────────────────

test_detect_linux_distribution() {
    printf "\n\033[1mTest: Linux distribution detection\033[0m\n"

    TEST_TMP=$(mktemp -d 2>/dev/null || mktemp -d -t 'azdo-test')
    printf 'ID=ubuntu\nVERSION_ID="24.04"\nPRETTY_NAME="Ubuntu 24.04.2 LTS"\n' > "$TEST_TMP/os-release"
    printf 'Linux version 6.8.0-generic\n' > "$TEST_TMP/kernel-release"

    WSL_DISTRO_NAME=""
    WSL_INTEROP=""
    VERSION="v2.0.0"
    detect_linux_distribution "$TEST_TMP/os-release" "$TEST_TMP/kernel-release" >/dev/null

    assert_eq "ubuntu" "$DISTRO_ID" "Distribution ID is detected"
    assert_eq "24.04" "$DISTRO_VERSION" "Distribution version is detected"
    assert_eq "Ubuntu 24.04.2 LTS" "$DISTRO_NAME" "Distribution name is detected"
    assert_eq "v2.0.0" "$VERSION" "Distribution detection preserves installer version"
    assert_eq "false" "$IS_WSL" "Native Linux is not reported as WSL"

    rm -rf "$TEST_TMP"
}

# ─── Test: WSL detection ──────────────────────────────────────────────────────

test_detect_wsl() {
    printf "\n\033[1mTest: WSL detection\033[0m\n"

    TEST_TMP=$(mktemp -d 2>/dev/null || mktemp -d -t 'azdo-test')
    printf 'Linux version 6.1.0-microsoft-standard-WSL2\n' > "$TEST_TMP/wsl-kernel"
    printf 'Linux version 6.8.0-generic\n' > "$TEST_TMP/native-kernel"

    WSL_DISTRO_NAME="Ubuntu-24.04"
    WSL_INTEROP=""
    detect_wsl "$TEST_TMP/native-kernel" >/dev/null
    assert_eq "true" "$IS_WSL" "WSL environment variable is detected"
    assert_eq "Ubuntu-24.04" "$WSL_DISTRO" "WSL distro name is reported"

    WSL_DISTRO_NAME=""
    WSL_INTEROP=""
    DISTRO_NAME=""
    detect_wsl "$TEST_TMP/wsl-kernel" >/dev/null
    assert_eq "true" "$IS_WSL" "Microsoft WSL kernel is detected"
    assert_eq "WSL" "$WSL_DISTRO" "WSL is reported when distro name is unavailable"

    detect_wsl "$TEST_TMP/native-kernel" >/dev/null
    assert_eq "false" "$IS_WSL" "Native Linux is not reported as WSL"

    rm -rf "$TEST_TMP"
}

# ─── Test: Version parsing ────────────────────────────────────────────────────

test_version_prefix() {
    printf "\n\033[1mTest: Version prefix handling\033[0m\n"

    # Test with 'v' prefix
    VERSION="v1.2.3"
    # Simulate the prefix logic from resolve_version
    case "$VERSION" in
        v*) ;;
        *)  VERSION="v$VERSION" ;;
    esac
    assert_eq "v1.2.3" "$VERSION" "Version with v prefix stays unchanged"

    # Test without 'v' prefix
    VERSION="1.2.3"
    case "$VERSION" in
        v*) ;;
        *)  VERSION="v$VERSION" ;;
    esac
    assert_eq "v1.2.3" "$VERSION" "Version without v prefix gets v prepended"
}

# ─── Test: Archive name construction ──────────────────────────────────────────

test_archive_name() {
    printf "\n\033[1mTest: Archive name construction\033[0m\n"

    BINARY_NAME="azdo"

    # Linux amd64
    VERSION="v1.0.0"
    VERSION_NUM="${VERSION#v}"
    OS_NAME="Linux"
    ARCH_NAME="x86_64"
    ARCHIVE_EXT="tar.gz"
    ARCHIVE_NAME="${BINARY_NAME}_${VERSION_NUM}_${OS_NAME}_${ARCH_NAME}.${ARCHIVE_EXT}"
    assert_eq "azdo_1.0.0_Linux_x86_64.tar.gz" "$ARCHIVE_NAME" "Linux amd64 archive name"

    # macOS arm64
    OS_NAME="Darwin"
    ARCH_NAME="arm64"
    ARCHIVE_NAME="${BINARY_NAME}_${VERSION_NUM}_${OS_NAME}_${ARCH_NAME}.${ARCHIVE_EXT}"
    assert_eq "azdo_1.0.0_Darwin_arm64.tar.gz" "$ARCHIVE_NAME" "macOS arm64 archive name"

    # Windows
    OS_NAME="Windows"
    ARCH_NAME="x86_64"
    ARCHIVE_EXT="zip"
    ARCHIVE_NAME="${BINARY_NAME}_${VERSION_NUM}_${OS_NAME}_${ARCH_NAME}.${ARCHIVE_EXT}"
    assert_eq "azdo_1.0.0_Windows_x86_64.zip" "$ARCHIVE_NAME" "Windows amd64 archive name"
}

# ─── Test: Install dir resolution ─────────────────────────────────────────────

test_install_dir_custom() {
    printf "\n\033[1mTest: Custom install directory\033[0m\n"

    INSTALL_DIR="/custom/path"
    resolve_install_dir >/dev/null 2>&1
    assert_eq "/custom/path" "$INSTALL_DIR" "Custom install dir is preserved"
}

test_install_dir_default() {
    printf "\n\033[1mTest: Default install directory\033[0m\n"

    INSTALL_DIR=""
    OS_NAME="Linux"
    resolve_install_dir >/dev/null 2>&1
    assert_not_empty "$INSTALL_DIR" "Default install dir is set"
}

# ─── Test: Parse args ─────────────────────────────────────────────────────────

test_parse_args() {
    printf "\n\033[1mTest: Argument parsing\033[0m\n"

    VERSION=""
    INSTALL_DIR=""

    parse_args --version v2.0.0 --install-dir /tmp/test
    assert_eq "v2.0.0" "$VERSION" "Version parsed from args"
    assert_eq "/tmp/test" "$INSTALL_DIR" "Install dir parsed from args"

    # Reset and test short flags
    VERSION=""
    INSTALL_DIR=""
    parse_args -v v3.0.0 -d /tmp/other
    assert_eq "v3.0.0" "$VERSION" "Version parsed from short flag"
    assert_eq "/tmp/other" "$INSTALL_DIR" "Install dir parsed from short flag"
}

# ─── Test: Download URL construction ──────────────────────────────────────────

test_download_url() {
    printf "\n\033[1mTest: Download URL construction\033[0m\n"

    REPO_OWNER="hari-incoming"
    REPO_NAME="hazdo"
    VERSION="v1.0.0"
    BINARY_NAME="azdo"
    GITHUB_DOWNLOAD="https://github.com"

    VERSION_NUM="${VERSION#v}"
    OS_NAME="Linux"
    ARCH_NAME="x86_64"
    ARCHIVE_EXT="tar.gz"
    ARCHIVE_NAME="${BINARY_NAME}_${VERSION_NUM}_${OS_NAME}_${ARCH_NAME}.${ARCHIVE_EXT}"

    DOWNLOAD_URL="$GITHUB_DOWNLOAD/$REPO_OWNER/$REPO_NAME/releases/download/$VERSION/$ARCHIVE_NAME"
    expected="https://github.com/hari-incoming/hazdo/releases/download/v1.0.0/azdo_1.0.0_Linux_x86_64.tar.gz"
    assert_eq "$expected" "$DOWNLOAD_URL" "Download URL is correctly constructed"
}

# ─── Test: macOS re-sign guard ────────────────────────────────────────────────

test_resign_macos_binary_skips_on_linux() {
    printf "\n\033[1mTest: macOS re-sign guard\033[0m\n"

    # Should be a silent no-op when OS_NAME is not Darwin, even if the binary
    # path doesn't exist — the function must short-circuit before touching it.
    OS_NAME="Linux"
    BINARY_NAME="azdo"
    out=$(resign_macos_binary /no/such/path 2>&1)
    rc=$?

    assert_eq "0" "$rc" "Returns 0 on non-Darwin"
    assert_eq "" "$out" "Produces no output on non-Darwin"
}

# ─── Run all tests ─────────────────────────────────────────────────────────────

main() {
    printf "\033[1mHazdo installer test suite\033[0m\n"
    printf "==============================\n"

    # Source the install script to get access to its functions
    # We override 'main' to prevent it from running
    SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
    (
        # Redefine main to a no-op before sourcing
        eval "$(sed 's/^main "\$@"$//' "$SCRIPT_DIR/install.sh")"

        test_detect_os
        test_detect_arch
        test_check_dependencies
        test_detect_linux_distribution
        test_detect_wsl
        test_version_prefix
        test_archive_name
        test_install_dir_custom
        test_install_dir_default
        test_parse_args
        test_download_url
        test_resign_macos_binary_skips_on_linux

        # Print summary
        printf "\n\033[1m==============================\033[0m\n"
        printf "Tests run:    %d\n" "$TESTS_RUN"
        printf "\033[0;32mTests passed: %d\033[0m\n" "$TESTS_PASSED"
        if [ "$TESTS_FAILED" -gt 0 ]; then
            printf "\033[0;31mTests failed: %d\033[0m\n" "$TESTS_FAILED"
            exit 1
        else
            printf "Tests failed: 0\n"
            printf "\n\033[0;32mAll tests passed!\033[0m\n"
        fi
    )
}

main
