.PHONY: build test test-unit lint fmt profile profile-report

# Mirrors the release gate's fixed run parameters (.github/workflows/release.yml)
# so a captured profile is comparable to what the gate measures.
PROFILE_GOMAXPROCS := 2
PROFILE_BENCHTIME := 200ms

build:
	mkdir -p bin
	go build -o bin/lispico ./cmd/lispico

test:
	go test ./...

test-unit: test

lint:
	golangci-lint run

fmt:
	go fmt ./...

profile:
	mkdir -p profiles
	GOMAXPROCS=$(PROFILE_GOMAXPROCS) GOLDSET_MODE=eval go test ./internal/goldset/ -run '^$$' -bench . -benchtime=$(PROFILE_BENCHTIME) -benchmem -cpuprofile=profiles/eval.cpu.prof -memprofile=profiles/eval.mem.prof -o profiles/eval.test
	GOMAXPROCS=$(PROFILE_GOMAXPROCS) GOLDSET_MODE=vm go test ./internal/goldset/ -run '^$$' -bench . -benchtime=$(PROFILE_BENCHTIME) -benchmem -cpuprofile=profiles/vm.cpu.prof -memprofile=profiles/vm.mem.prof -o profiles/vm.test

# alloc_space (total allocation traffic), not the inuse_space default -- a
# benchmark's objects are dead by the time the profile is written, so
# inuse_space would show next to nothing.
profile-report:
	go tool pprof -top -nodecount=20 profiles/eval.test profiles/eval.cpu.prof
	go tool pprof -top -nodecount=20 -alloc_space profiles/eval.test profiles/eval.mem.prof
	go tool pprof -top -nodecount=20 profiles/vm.test profiles/vm.cpu.prof
	go tool pprof -top -nodecount=20 -alloc_space profiles/vm.test profiles/vm.mem.prof
