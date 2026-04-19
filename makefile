APP_NAME := moonlight-server
BUILD_DIR := build
SRC_DIR := ./src
VERSION ?= $(shell git describe --tags --always)

.PHONY: all build clean run dev install

all: build

build:
	mkdir -p $(BUILD_DIR)
	go build -ldflags "-X main.Version=$(VERSION)" -o $(BUILD_DIR)/$(APP_NAME) $(SRC_DIR)

clean:
	rm -rf $(BUILD_DIR)

run: build
	./$(BUILD_DIR)/$(APP_NAME)

dev:
	go run github.com/swaggo/swag/cmd/swag@latest init -d ./src
	go run ./src

install: build
	sudo cp ./$(BUILD_DIR)/$(APP_NAME) /usr/bin/$(APP_NAME)
