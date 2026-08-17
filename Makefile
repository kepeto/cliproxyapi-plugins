CGO ?= 1
SO ?= nous-portal.so

.PHONY: build verify clean

build:
	CGO_ENABLED=$(CGO) go build -buildmode=c-shared -o $(SO) .

verify: build
	gcc -D'SO="$(CURDIR)/$(SO)"' -O2 -o verify verify.c -ldl
	./verify

clean:
	rm -f $(SO) nous-portal.h verify
