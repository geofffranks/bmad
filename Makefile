VERSION=1.0.1
DISTFILE=bmad-${VERSION}.tar.gz


DEFAULT:
	go install
test:
	go test github.com/geofffranks/bmad github.com/geofffranks/bmad/bma  github.com/geofffranks/bmad/log
version:
	@echo ${VERSION}
distfile:
	@echo ${DISTFILE}
dist: clean
	@tar czf ${DISTFILE} *
	@echo ${DISTFILE}
docs:
	mkdir -p doc
	mango-doc -version=${VERSION}     > doc/bmad.1
	mango-doc -version=${VERSION} bma > doc/bmad-bma.3
	mango-doc -version=${VERSION} log > doc/bmad-log.3

clean:
	rm -rf doc
	rm -rf obj-x86_64-linux-gnu
