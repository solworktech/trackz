#!/bin/bash

# Trackz Visualization - Build Script
# This script handles common build tasks

set -e  # Exit on error

PROJECT_NAME="trackz-viz"
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

print_status() {
    echo -e "${GREEN}➜${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

# Check if Rust is installed
check_rust() {
    if ! command -v cargo &> /dev/null; then
        print_error "Rust is not installed!"
        echo "Install from: https://rustup.rs/"
        exit 1
    fi
    print_status "Rust found: $(cargo --version)"
}

# Build the project
build() {
    print_status "Building $PROJECT_NAME in release mode..."
    cargo build --release
    print_status "Build complete! Binary at: target/release/$PROJECT_NAME"
}

# Run the application
run() {
    if [ ! -f "target/release/$PROJECT_NAME" ]; then
        print_warning "Binary not found. Building first..."
        build
    fi
    
    if [ ! -f "trackz.db" ]; then
        print_warning "trackz.db not found in current directory"
        print_status "Continuing anyway (app will show error if DB is needed)"
    fi
    
    print_status "Starting $PROJECT_NAME..."
    ./target/release/$PROJECT_NAME
}

# Format code
format() {
    print_status "Formatting code..."
    cargo fmt
    print_status "Code formatted"
}

# Run clippy linter
lint() {
    print_status "Running clippy linter..."
    cargo clippy -- -D warnings
    print_status "Linting complete"
}

# Run tests
test() {
    print_status "Running tests..."
    cargo test
    print_status "Tests complete"
}

# Clean build artifacts
clean() {
    print_status "Cleaning build artifacts..."
    cargo clean
    print_status "Clean complete"
}

# Show usage
usage() {
    cat << EOF
Usage: ./build.sh [COMMAND]

Commands:
    build       Build project in release mode (default)
    run         Build and run the application
    format      Format code with rustfmt
    lint        Run clippy linter
    test        Run unit tests
    clean       Remove build artifacts
    help        Show this help message

Examples:
    ./build.sh build      # Just build the binary
    ./build.sh run        # Build and run
    ./build.sh format     # Format code

Environment Variables:
    TRACKZ_DB             # Path to trackz database (default: ./trackz.db)
                          # Example: TRACKZ_DB=/path/to/trackz.db ./build.sh run

EOF
}

# Main logic
main() {
    check_rust
    cd "$SCRIPT_DIR"
    
    case "${1:-build}" in
        build)
            build
            ;;
        run)
            run
            ;;
        format)
            format
            ;;
        lint)
            lint
            ;;
        test)
            test
            ;;
        clean)
            clean
            ;;
        help|--help|-h)
            usage
            ;;
        *)
            print_error "Unknown command: $1"
            usage
            exit 1
            ;;
    esac
}

main "$@"
