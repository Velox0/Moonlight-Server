.PHONY: build run clean proto proto-go proto-js proto-python install-deps test

# Go build settings
GO_MODULE := github.com/velox0/moonlight-server
BUILD_DIR := build
BINARY_NAME := moonlight-server
SRC_DIR := src

# Protobuf settings
PROTO_DIR := moonlight_server_proto
PROTO_OUT_DIR := moonlight_server_proto
PROTO_FILE := moonlight.proto
OUT_DIR := .

# Default target
all: proto build

# Build the Go binary
build: proto-go
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	cd $(SRC_DIR) && go build -o ../$(BUILD_DIR)/$(BINARY_NAME) .
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Run the server
run: build
	@echo "Starting Moonlight server..."
	./$(BUILD_DIR)/$(BINARY_NAME)

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	rm -f $(PROTO_OUT_DIR)/*.pb.go
	rm -f clients/*_pb.js
	rm -f clients/*_pb2.py
	rm -f clients/*_pb2_grpc.py
	rm -f clients/*.proto

# Install required dependencies
install-deps:
	@echo "Installing protoc plugins..."
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

	make proto
	@echo "Installing Go dependencies..."
	cd $(SRC_DIR) && go mod tidy
	
	@echo "Installing Node.js dependencies..."
	cd clients && npm install @grpc/grpc-js @grpc/proto-loader express

# Generate all protobuf files
proto: proto-go proto-js

# Generate Go protobuf files
proto-go:
	@echo "Generating Go protobuf files..."
	protoc --proto_path=$(PROTO_DIR) \
		--go_out=$(PROTO_OUT_DIR) \
		--go_opt=paths=source_relative \
		--go-grpc_out=$(PROTO_OUT_DIR) \
		--go-grpc_opt=paths=source_relative \
		$(PROTO_DIR)/$(PROTO_FILE)

# Generate JavaScript protobuf files for Node.js clients
proto-js:
	cp $(PROTO_DIR)/moonlight.proto clients/
	@echo "Generating JavaScript protobuf files..."
	mkdir -p clients
	protoc --proto_path=$(PROTO_DIR) \
		--js_out=import_style=commonjs,binary:clients \
		--grpc-web_out=import_style=commonjs,mode=grpcwebtext:clients \
		$(PROTO_DIR)/$(PROTO_FILE)

# Test the server
test:
	@echo "Running tests..."
	cd $(SRC_DIR) && go test -v ./...

# Format code
fmt:
	@echo "Formatting Go code..."
	cd $(SRC_DIR) && go fmt ./...
	
	@echo "Formatting JavaScript code..."
	cd clients && npx prettier --write "*.js"

# Lint code
lint:
	@echo "Linting Go code..."
	cd $(SRC_DIR) && golangci-lint run

# Create systemd service file
# systemd-service:
# 	@echo "Creating systemd service file..."
# 	@cat > moonlight.service << 'EOF'
# [Unit]
# Description=Moonlight gRPC/HTTP Server
# After=network.target

# [Service]
# Type=simple
# User=moonlight
# Group=moonlight
# WorkingDirectory=/opt/moonlight
# ExecStart=/opt/moonlight/moonlight-server
# Restart=always
# RestartSec=5
# StandardOutput=journal
# StandardError=journal

# [Install]
# WantedBy=multi-user.target
# EOF
# 	@echo "Created moonlight.service"
# 	@echo "To install: sudo cp moonlight.service /etc/systemd/system/"
# 	@echo "To enable: sudo systemctl enable moonlight"
# 	@echo "To start: sudo systemctl start moonlight"

# Install server to system
install: build
	@echo "Installing Moonlight server..."
	sudo mkdir -p /opt/moonlight
	sudo cp $(BUILD_DIR)/$(BINARY_NAME) /opt/moonlight/
	sudo mkdir -p /etc/moonlight
	@if [ ! -f /etc/moonlight/mls.json ]; then \
		echo "Installing default configuration..."; \
		sudo cp mls.json /etc/moonlight/mls.json; \
	else \
		echo "Configuration already exists at /etc/moonlight/mls.json"; \
	fi
	@echo "Installation complete!"
	@echo "Edit /etc/moonlight/mls.json to configure the server"

# Development server with auto-restart
dev:
	@echo "Starting development server with auto-restart..."
	@which air > /dev/null || (echo "Installing air..." && go install github.com/cosmtrek/air@latest)
	cd $(SRC_DIR) && air

# Docker build
docker:
	@echo "Building Docker image..."
	docker build -t moonlight-server .

# Docker run
docker-run: docker
	@echo "Running Docker container..."
	docker run -p 8080:8080 -p 8081:8081 \
		-v /etc/moonlight:/etc/moonlight:ro \
		moonlight-server

# Check if protoc is installed
check-protoc:
	@which protoc > /dev/null || (echo "Error: protoc not found. Please install Protocol Buffers compiler." && exit 1)
	@echo "protoc found: $(shell protoc --version)"

# Setup development environment
setup: check-protoc install-deps
	@echo "Setting up development environment..."
	@echo "Creating example configuration..."
	@mkdir -p /tmp/moonlight
	@cp mls.json /tmp/moonlight/mls.json
	@echo "Development environment setup complete!"
	@echo "You can now run: make dev"

# Show help
help:
	@echo "Available targets:"
	@echo "  build          - Build the server binary"
	@echo "  run            - Build and run the server"
	@echo "  clean          - Clean build artifacts"
	@echo "  proto          - Generate all protobuf files"
	@echo "  proto-go       - Generate Go protobuf files"
	@echo "  proto-js       - Generate JavaScript protobuf files"
	@echo "  proto-python   - Generate Python protobuf files"
	@echo "  install-deps   - Install required dependencies"
	@echo "  test           - Run tests"
	@echo "  fmt            - Format code"
	@echo "  lint           - Lint code"
	@echo "  install        - Install server to system"
	@echo "  systemd-service- Create systemd service file"
	@echo "  dev            - Start development server with auto-restart"
	@echo "  docker         - Build Docker image"
	@echo "  docker-run     - Build and run Docker container"
	@echo "  setup          - Setup development environment"
	@echo "  help           - Show this help"