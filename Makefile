.PHONY: all build build-monitor clean install uninstall test run

BINARY=sflow-enricher
MONITOR=sflow-monitor
VERSION=2.3.0
BUILD_DIR=build
CONFIG_DIR=/etc/sflow-enricher
INSTALL_DIR=/usr/local/bin

all: build build-monitor

build:
	@echo "Building $(BINARY)..."
	@mkdir -p $(BUILD_DIR)
	go build -ldflags "-X main.version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY) ./cmd/sflow-enricher

build-monitor:
	@echo "Building $(MONITOR)..."
	@mkdir -p $(BUILD_DIR)
	go build -ldflags "-X main.monitorVersion=$(VERSION)" -o $(BUILD_DIR)/$(MONITOR) ./cmd/sflow-monitor

build-static:
	@echo "Building static $(BINARY) + $(MONITOR)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux go build -ldflags "-X main.version=$(VERSION) -s -w" -o $(BUILD_DIR)/$(BINARY) ./cmd/sflow-enricher
	CGO_ENABLED=0 GOOS=linux go build -ldflags "-X main.monitorVersion=$(VERSION) -s -w" -o $(BUILD_DIR)/$(MONITOR) ./cmd/sflow-monitor

clean:
	@echo "Cleaning..."
	rm -rf $(BUILD_DIR)

install: build build-monitor
	@echo "Installing..."
	@mkdir -p $(CONFIG_DIR)
	install -m 755 $(BUILD_DIR)/$(BINARY) $(INSTALL_DIR)/$(BINARY)
	install -m 755 $(BUILD_DIR)/$(MONITOR) $(INSTALL_DIR)/$(MONITOR)
	@if [ ! -f $(CONFIG_DIR)/config.yaml ]; then \
		install -m 644 config.yaml $(CONFIG_DIR)/config.yaml; \
	else \
		echo "Config file exists, not overwriting"; \
	fi
	@if [ -d /etc/systemd/system ]; then \
		install -m 644 systemd/sflow-enricher.service /etc/systemd/system/; \
		systemctl daemon-reload; \
		echo "Systemd service installed. Run: systemctl enable --now sflow-enricher"; \
	fi

uninstall:
	@echo "Uninstalling..."
	systemctl stop sflow-enricher 2>/dev/null || true
	systemctl disable sflow-enricher 2>/dev/null || true
	rm -f /etc/systemd/system/sflow-enricher.service
	rm -f $(INSTALL_DIR)/$(BINARY)
	rm -f $(INSTALL_DIR)/$(MONITOR)
	systemctl daemon-reload
	@echo "Config left in $(CONFIG_DIR) - remove manually if needed"

test:
	go test -v ./...

run: build
	$(BUILD_DIR)/$(BINARY) -config config.yaml -debug

deps:
	go mod download
	go mod tidy
