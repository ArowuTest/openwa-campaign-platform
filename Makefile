.PHONY: format fmt test race vet build frontend-syntax traceability governance-check governance-test deployment-topology admin-web-hosting admin-web-deployment-packet deployment-readiness release-gate check tree package bundle

VERSION := $(shell cat VERSION)

format:
	gofmt -w cmd internal tests

fmt:
	@test -z "$$(gofmt -l cmd internal tests)" || (echo "Go files require formatting:"; gofmt -l cmd internal tests; exit 1)

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

build:
	mkdir -p bin
	@set -e; for command in $$(find cmd -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | sort); do \
		echo "building $$command"; \
		go build -trimpath -o "bin/$$command" "./cmd/$$command"; \
	done

frontend-syntax:
	node scripts/check-typescript-syntax.js

traceability:
	python3 scripts/generate-traceability.py
	git diff --exit-code -- docs/requirements/TRACEABILITY.md docs/requirements/traceability.json

governance-check:
	python3 scripts/verify-release-readiness.py

deployment-topology:
	python3 scripts/verify-deployment-topology.py

admin-web-hosting:
	python3 scripts/verify-admin-web-hosting.py

admin-web-deployment-packet:
	python3 scripts/verify-admin-web-deployment-packet.py

deployment-readiness:
	python3 scripts/verify-deployment-readiness.py

railway-production-shell:
	python3 scripts/verify-railway-production-shell.py

railway-production-variable-plan:
	python3 scripts/verify-railway-production-variable-plan.py

railway-production-image-plan:
	python3 scripts/verify-railway-production-image-plan.py

railway-production-postgres-plan:
	python3 scripts/verify-railway-production-postgres-plan.py

railway-service-config:
	python3 scripts/verify-railway-service-config.py

railway-db-identity-variables:
	python3 scripts/verify-railway-db-identity-variables.py

governance-test:
	python3 -m unittest discover -s tests/governance -p "test_*.py"

release-gate:
	python3 scripts/verify-release-readiness.py --production-candidate

check: fmt test race vet build frontend-syntax traceability governance-check governance-test deployment-topology admin-web-hosting admin-web-deployment-packet deployment-readiness railway-production-shell railway-production-variable-plan railway-production-image-plan railway-production-postgres-plan railway-service-config railway-db-identity-variables

tree:
	find . -maxdepth 4 -type f | sort

package:
	rm -f ../campaign-platform-$(VERSION)-source.zip
	zip -qr ../campaign-platform-$(VERSION)-source.zip . \
		-x '.git/*' 'node_modules/*' '.next/*' 'dist/*' 'bin/*' '*.log' '.env'

bundle:
	git bundle create ../campaign-platform-$(VERSION).bundle --all
