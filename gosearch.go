// Copyright 2015 Peter Mattis.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License. See the AUTHORS file
// for names of contributors.

// TODO(pmattis): Watch for changes to the go source
// directories. Index git repositories and any non-tracked source
// files. Keep a list of the files that have changed but not been
// re-indexed. When this list gets too large, kick off a background
// indexing to merge in the changes. The changed but not yet indexed
// files need to be scanned for each query.
//
// Keep a mapping from from import name to package. Use
// go/build.Import to determine the import name for a package. Allow
// querying for matching packages.

package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/build"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/libgit2/git2go"
	"github.com/petermattis/codesearch/index"
	"github.com/petermattis/codesearch/regexp"
)

var (
	indexFlag         = flag.Bool("i", false, "index repositories")
	filenamesOnlyFlag = flag.Bool("l", false, "print filenames only")
	countFlag         = flag.Bool("c", false, "print count of matches")
	lineNumbersFlag   = flag.Bool("n", false, "print line numbers")
	noFilenamesFlag   = flag.Bool("h", false, "do not print filenames")
)

func indexGit(ix *index.IndexWriter, dir string) error {
	repo, err := git.OpenRepository(dir)
	if err != nil {
		return err
	}

	head, err := repo.Head()
	if err != nil {
		return err
	}
	defer head.Free()

	fmt.Printf("%s: %s\n", dir, head.Target())

	repo.ForeachSubmodule(func(sub *git.Submodule, name string) int {
		indexGit(ix, filepath.Join(dir, sub.Path()))
		return 0
	})

	commit, err := repo.LookupCommit(head.Target())
	if err != nil {
		return err
	}
	defer commit.Free()

	tree, err := commit.Tree()
	if err != nil {
		return err
	}
	defer tree.Free()

	tree.Walk(func(name string, entry *git.TreeEntry) int {
		if entry.Type != git.ObjectBlob {
			return 0
		}

		path := dir + "#" + name + entry.Name

		blob, err := repo.LookupBlob(entry.Id)
		if err != nil {
			fmt.Printf("%s: unable to lookup blob: %s\n", path, err)
			return 0
		}
		defer blob.Free()

		if blob.Size() >= 1<<20 {
			fmt.Printf("%s: skipping large file\n", path)
			return 0
		}

		contents := blob.Contents()
		if contents == nil {
			fmt.Printf("%s: unable to lookup blob: %s\n", path, err)
			return 0
		}

		fmt.Printf("%s#%s %d\n", path, entry.Id, len(contents))
		ix.Add(path+"#"+entry.Id.String(), bytes.NewReader(contents))
		return 0
	})

	return nil
}

// A gitBlob contains the info for a single blob in a git repository.
type gitBlob struct {
	dir  string // git repo directory
	path string // path within the repository the blob is associated with
	sha  string // sha of the blob (i.e. the object id)
}

// parseBlob returns a new gitBlob that has been parsed from a path
// returned from fullPath.
func parseBlob(path string) gitBlob {
	parts := strings.Split(path, "#")
	if len(parts) != 3 {
		log.Fatalf("unable to parse blob path: %s", path)
	}
	return gitBlob{
		dir:  parts[0],
		path: parts[1],
		sha:  parts[2],
	}
}

func (b gitBlob) String() string {
	return b.fullPath()
}

// fullPath returns a string containing all of the components of a blob. A
// gitBlob can be reconstituted from this strong using parseBlob.
func (b gitBlob) fullPath() string {
	return filepath.Join(b.dir, b.path, b.sha)
}

// prettyPath returns a path which excludes the trailing sha component
// and is thus more suitable for display to a user.
func (b gitBlob) prettyPath() string {
	return filepath.Join(b.dir, b.path)
}

// queryIndex performs a query against the most recent index,
// returning a list of paths that potentially match the query. Note
// that the paths are suffixed with "/<sha>" so they do not exist on
// disk.
func queryIndex(query *index.Query) []gitBlob {
	ix := index.Open("./csearchindex")
	defer ix.Close()

	post := ix.PostingQuery(query)

	var names []gitBlob
	for _, fileid := range post {
		names = append(names, parseBlob(ix.Name(fileid)))
	}
	return names
}

func searchGit(query string) error {
	// Compile codesearch regular expressions
	re, err := regexp.Compile(query)
	if err != nil {
		return err
	}

	// Get list of file blobs to grep from the index
	indexQuery := index.RegexpQuery(re.Syntax)
	blobs := queryIndex(indexQuery)

	// Grep over files returned using codesearch grep.  Errors at this point are
	// not fatal.
	g := regexp.Grep{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Regexp: re,
		L:      *filenamesOnlyFlag && !*countFlag,
		C:      *countFlag,
		N:      *lineNumbersFlag,
		H:      *noFilenamesFlag,
	}

	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	var repo *git.Repository
	lastdir := ""
	for _, blob := range blobs {
		// Open a different repository if different than the last blob.
		if blob.dir != lastdir {
			if repo != nil {
				repo.Free()
				repo = nil
			}

			repo, err = git.OpenRepository(blob.dir)
			if err != nil {
				fmt.Printf("%s: %s\n", blob.dir, err)
				continue
			}

			lastdir = blob.dir
		}

		id, err := git.NewOid(blob.sha)
		if err != nil {
			fmt.Printf("%s: %s\n", blob.prettyPath(), err)
			continue
		}

		data, err := repo.LookupBlob(id)
		if err != nil {
			fmt.Printf("%s: %s\n", blob.prettyPath(), err)
			continue
		}

		path, err := filepath.Rel(wd, blob.prettyPath())
		if err != nil {
			fmt.Printf("%s: %s\n", blob.prettyPath(), err)
			continue
		}

		g.Reader(bytes.NewReader(data.Contents()), path)

		data.Free()
	}

	if repo != nil {
		repo.Free()
	}
	return nil
}

func main() {
	if false {
		flag.Parse()
		args := flag.Args()

		for _, arg := range args {
			path, err := filepath.Abs(arg)
			if err != nil {
				fmt.Printf("%s\n", err)
				continue
			}
			pkg, err := build.Import(".", path, 0)
			if err != nil {
				fmt.Printf("%s\n", err)
			}
			fmt.Printf("%s %s\n", pkg.Name, pkg.ImportPath)
		}
	}

	if false {
		s := NewFSEvents(fseventsNow, 100*time.Millisecond, NoDefer|FileEvents,
			build.Default.SrcDirs()...)
		s.Start()
		defer s.Close()

		for {
			select {
			case e := <-s.Chan:
				fmt.Printf("%v: %s: %s\n", e.ID, e.Flags, e.Path)
			}
		}
	}

	if true {
		flag.Parse()
		args := flag.Args()

		if *indexFlag {
			ix := index.Create("./csearchindex")
			ix.LogSkip = true
			ix.Verbose = true
			for _, arg := range args {
				path, err := filepath.Abs(arg)
				if err != nil {
					fmt.Printf("%s\n", err)
					continue
				}
				if err := indexGit(ix, path); err != nil {
					fmt.Printf("%s\n", err)
					continue
				}
			}
			ix.Flush()
		} else {
			for _, arg := range args {
				searchGit(arg)
			}
		}
	}
}
