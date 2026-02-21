.PHONY: build test install clean run

build:
	go build -o captainslog ./cmd/captainslog

test:
	go test ./...

install: build
	cp captainslog $(HOME)/.local/bin/captainslog
	cp captainslog-cli $(HOME)/.local/bin/captainslog-cli
	@echo "Installed to ~/.local/bin/"

clean:
	rm -f captainslog

run: build
	./captainslog

service:
	cp examples/captainslog.service $(HOME)/.config/systemd/user/captainslog.service
	systemctl --user daemon-reload
	systemctl --user enable --now captainslog
	@echo "Service enabled and started"
