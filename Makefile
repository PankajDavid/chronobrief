.PHONY: doctor check test lint typecheck go-test invariants pert e2e

doctor:            ## verify the machine can build and run everything
	python scripts/doctor.py

test:              ## python tests
	python -m pytest

go-test:           ## pure decision logic tests (no Bifrost needed)
	cd gateway/plugins/freshcache && go test ./internal/...

lint:
	ruff check .

typecheck:
	mypy chronobrief

evidence:          ## docs must quote live-overhead numbers computed from docs/evidence/live-overhead-25runs.txt
	python scripts/check_evidence.py

invariants:        ## static guards: plugin purity, no st.cache, no secrets, pack lint
	python scripts/check_invariants.py

check: invariants evidence lint test go-test   ## run before claiming anything is done

pert:              ## critical path and remaining time; HOURS=6 make pert
	python scripts/pert.py $(if $(HOURS),--hours $(HOURS),)

e2e:               ## full MISS, HIT, REFUSE cycle against the real gateway (built in /cb-wire-gateway)
	@test -f scripts/e2e_cache.py || (echo "scripts/e2e_cache.py does not exist yet: build it with /cb-wire-gateway" && exit 1)
	python scripts/e2e_cache.py
