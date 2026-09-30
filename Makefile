.PHONY: all build build-linux-amd64 build-linux-arm64 fyne-app fyne-distrobox test run deploy clean help

BIN_DIR := bin
API_SERVER_BIN := $(BIN_DIR)/api-server
CTL_BIN := $(BIN_DIR)/neon-ctl
FYNE_BIN := $(BIN_DIR)/fyne-manager

all: build

build:
	@mkdir -p $(BIN_DIR)
	@echo "==> Building api-server..."
	go build -o $(API_SERVER_BIN) ./cmd/api-server
	@echo "==> Building neon-ctl..."
	go build -o $(CTL_BIN) ./cmd/neon-ctl
	@echo "[+] Built: $(API_SERVER_BIN), $(CTL_BIN)"

build-linux-amd64:
	@mkdir -p $(BIN_DIR)
	@echo "==> Cross-compiling for Linux amd64..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o $(BIN_DIR)/api-server-linux-amd64 ./cmd/api-server
	@echo "[+] Built: $(BIN_DIR)/api-server-linux-amd64"

build-linux-arm64:
	@mkdir -p $(BIN_DIR)
	@echo "==> Cross-compiling for Linux arm64..."
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o $(BIN_DIR)/api-server-linux-arm64 ./cmd/api-server
	@echo "[+] Built: $(BIN_DIR)/api-server-linux-arm64"

fyne-app:
	@mkdir -p $(BIN_DIR)
	@echo "==> Building Fyne Desktop Manager..."
	@echo "Note: If on an immutable host like Bazzite, run 'make fyne-distrobox'"
	go build -o $(FYNE_BIN) ./cmd/fyne-manager || $(MAKE) fyne-distrobox
	@echo "[+] Built: $(FYNE_BIN)"

fyne-distrobox:
	@mkdir -p $(BIN_DIR)
	@echo "==> Building Fyne Desktop Manager inside Distrobox (dev)..."
	distrobox enter dev -- go build -o $(FYNE_BIN) ./cmd/fyne-manager
	@echo "[+] Built: $(FYNE_BIN)"

run: build
	@echo "==> Starting local API server in development mode..."
	./$(API_SERVER_BIN) -config config.example.yaml -init-admin

test:
	@echo "==> Running Go unit and integration tests..."
	go test -v ./internal/...

deploy:
	@echo "==> Deploying to Ubuntu server over SSH..."
	./deploy/deploy.sh

clean:
	@echo "==> Cleaning build artifacts..."
	rm -rf $(BIN_DIR) data/

help:
	@echo "NeonServices Build & Deploy Commands:"
	@echo "  make build               - Build local api-server and neon-ctl binaries"
	@echo "  make build-linux-amd64   - Cross-compile static Linux x86_64 binary"
	@echo "  make build-linux-arm64   - Cross-compile static Linux ARM64 binary"
	@echo "  make fyne-distrobox      - Build Fyne GUI app inside Distrobox container"
	@echo "  make fyne-app            - Build Fyne Desktop GUI Control Center"
	@echo "  make run                 - Run api-server locally with test admin"
	@echo "  make test                - Run unit and integration tests"
	@echo "  make deploy              - Build and deploy to remote Ubuntu host via SSH"
	@echo "  make clean               - Clean bin/ and temporary build files"
