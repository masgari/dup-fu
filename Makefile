.PHONY: all
all: build build-windows

build:
	go build -o $(GOPATH)/bin/dup-fu main.go

build-windows:
	GOOS=windows GOARCH=386 go build -o dup-fu.exe main.go

clean:
	rm -f dup-fu dup-fu.exe
