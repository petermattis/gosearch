all: build

build:
	make -C ../../libgit2/git2go install
	go install .
