.PHONY: hooks check test fmt vet history

# core.hooksPath is per-clone config, so every clone runs this once.
hooks:
	@git config core.hooksPath .githooks
	@chmod +x .githooks/*
	@echo "hooks active: $$(git config core.hooksPath)"

check: fmt vet test

fmt:
	@test -z "$$(gofmt -l . | grep -v '^$$')" || { echo "gofmt would change:"; gofmt -l .; exit 1; }

vet:
	@go vet ./...

test:
	@go test ./... -cover

# Every commit on this branch must build and pass on its own.
history:
	@w=$$(mktemp -d); \
	for c in $$(git rev-list --reverse origin/main..HEAD 2>/dev/null || git rev-list --reverse HEAD); do \
	  rm -rf $$w; git worktree add -q --detach $$w $$c; \
	  if [ ! -f $$w/go.mod ]; then \
	    printf "  skip  %s  %s (no go module yet)\n" "$${c:0:7}" "$$(git log -1 --format=%s $$c)"; \
	  elif (cd $$w && go build ./... >/dev/null 2>&1 && go test ./... >/dev/null 2>&1); then \
	    printf "  ok    %s  %s\n" "$${c:0:7}" "$$(git log -1 --format=%s $$c)"; \
	  else \
	    printf "  FAIL  %s  %s\n" "$${c:0:7}" "$$(git log -1 --format=%s $$c)"; \
	  fi; \
	  git worktree remove --force $$w 2>/dev/null; \
	done; rm -rf $$w
