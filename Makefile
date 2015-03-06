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
dist: clean
	@tar czf ${DISTFILE} *
	@echo ${DISTFILE}
docs:
	mkdir -p doc
	mango-doc -version=${VERSION}     > doc/bmad.1
	mango-doc -version=${VERSION} bma > doc/bmad-bma.3

clean:
	rm -rf doc
	rm -rf obj-x86_64-linux-gnu
