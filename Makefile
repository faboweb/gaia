all: get_vendor_deps test install

test:
	@go test `glide novendor`

install:
	go install ./cmd/...

get_vendor_deps:
	go get github.com/Masterminds/glide
	glide install

build_binaries:
	go get github.com/karalabe/xgo
	xgo -dest -targets=darwin/*,linux/*,windows/* build/ ./cmd/gaia
	xgo -dest build/ ./cmd/gaiacli
