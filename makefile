APP_NAME := moonlight-server
BUILD_DIR := build
SRC_DIR := ./src

.PHONY: all build clean run

all: build

build:
	mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(APP_NAME) $(SRC_DIR)

clean:
	rm -rf $(BUILD_DIR)

run: build
	./$(BUILD_DIR)/$(APP_NAME)
