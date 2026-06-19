BINDIR := bin
DISTDIR := dist
CMDS   := ft8ctrl lookup countries adif
GOFLAGS :=
LDFLAGS := -s -w

# Target platforms for `make release`. Pure-Go (CGO disabled) so every binary is
# statically linked and needs no SQLite system library.
PLATFORMS := linux/amd64 linux/arm64 darwin/arm64 darwin/amd64

.PHONY: all build test lint vet fmt tidy clean release $(CMDS)

all: build

build: $(CMDS)

$(CMDS):
	@mkdir -p $(BINDIR)
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BINDIR)/$@ ./cmd/$@

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -s -w .

lint:
	golangci-lint run

tidy:
	go mod tidy

release:
	@mkdir -p $(DISTDIR)
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		for cmd in $(CMDS); do \
			out=$(DISTDIR)/$$cmd-$$os-$$arch; \
			echo "building $$out"; \
			CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
				go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $$out ./cmd/$$cmd || exit 1; \
		done; \
	done

clean:
	rm -rf $(BINDIR) $(DISTDIR)
