VERSION=1.0.0
DISTFILE=bmad-${VERSION}.tar.gz


DEFAULT:
	go install
test:
	go test
version:
	@echo ${VERSION}
distfile:
	@echo ${DISTFILE}
dist:
	@tar czf ${DISTFILE} *
	@echo ${DISTFILE}
