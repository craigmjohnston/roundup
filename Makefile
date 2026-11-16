MYGO_VERSION := v0.9.0
MYGO_DIR := third_party/mygo
LABEL := local.roundup
AGENT := $(HOME)/Library/LaunchAgents/$(LABEL).plist

.PHONY: build test mygo install-agent uninstall-agent restart-agent

build: $(MYGO_DIR)
	go build -o bin/roundup ./cmd/roundup
	go build -o bin/roundup-slack ./plugins/slack/cmd/roundup-slack
	go build -o bin/roundup-github ./plugins/github/cmd/roundup-github
	go build -o bin/slk ./plugins/slack/cmd/slk

test: $(MYGO_DIR)
	go test ./... ./plugins/...
	go vet ./... ./plugins/...
	test -z "$$(gofmt -l $$(git ls-files '*.go'))"

mygo: $(MYGO_DIR)

# The patch lets a coloured menu bar icon keep its own size and have a highlighted variant.
$(MYGO_DIR): patches/mygo-$(MYGO_VERSION)-tray-icon.patch
	rm -rf $@
	mkdir -p $(dir $@)
	cp -R "$$(GOFLAGS=-mod=mod go mod download -json github.com/egoist/mygo@$(MYGO_VERSION) | sed -n 's/.*"Dir": "\(.*\)",/\1/p')" $@
	chmod -R u+w $@
	patch -d $@ -p1 < $<

install-agent: build
	sed -e 's|@LABEL@|$(LABEL)|g' -e 's|@BINARY@|$(CURDIR)/bin/roundup|g' -e 's|@LOG@|$(HOME)/Library/Logs/roundup.log|g' launchd/roundup.plist.in > $(AGENT)
	@launchctl bootout gui/$$(id -u)/$(LABEL) 2>/dev/null || true
	@for i in $$(seq 50); do launchctl print gui/$$(id -u)/$(LABEL) >/dev/null 2>&1 || break; sleep 0.1; done
	launchctl bootstrap gui/$$(id -u) $(AGENT)

uninstall-agent:
	-launchctl bootout gui/$$(id -u)/$(LABEL)
	rm -f $(AGENT)

restart-agent: build
	launchctl kickstart -k gui/$$(id -u)/$(LABEL)
