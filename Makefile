.PHONY: build nginx ensure-nginx discover

# Default: nginx ensure then go build (same as scripts/build.sh)
build:
	bash scripts/build.sh

nginx ensure-nginx:
	bash scripts/ensure-nginx.sh

discover:
	bash scripts/ensure-nginx.sh --discover
