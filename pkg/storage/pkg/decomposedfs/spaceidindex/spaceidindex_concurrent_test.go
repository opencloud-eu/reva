// Copyright 2026 Michael Bortlik <michael@bortlik.io>
// SPDX-License-Identifier: Apache-2.0

package spaceidindex_test

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opencloud-eu/reva/v3/pkg/storage/pkg/decomposedfs/spaceidindex"
	"github.com/vmihailenco/msgpack/v5"
)

// The concurrency tests below let several writers add entries to the same index at the same time, each writer with
// its own Index instance and therefore its own file handles, and check that no entry gets lost.
//
// By default they run in a temporary directory. To run them on a shared filesystem from several nodes at once
// (e.g. CephFS mounted with the kernel client on two machines), start the test binary on every node at the same
// time with
//
//	SPACEIDINDEX_TEST_DIR=<directory on the shared filesystem, the same on every node, new for every run>
//	SPACEIDINDEX_TEST_NODES=<all node names, comma separated, e.g. a,b>
//	SPACEIDINDEX_TEST_NODE=<name of this node>
//
// Every node then waits for the others and checks the entries of all nodes. Optional:
// SPACEIDINDEX_TEST_WRITERS (writers per node, default 4), SPACEIDINDEX_TEST_ENTRIES (entries per writer,
// default 50), SPACEIDINDEX_TEST_PAUSE (pause between two adds of a writer, Go duration, default 0).

const (
	helperEnv     = "SPACEIDINDEX_TEST_HELPER"
	loopHelperEnv = "SPACEIDINDEX_TEST_LOOP_HELPER"
)

type setup struct {
	dir     string
	node    string
	nodes   []string
	writers int
	entries int
	pause   time.Duration
}

func newSetup(t *testing.T) setup {
	t.Helper()
	s := setup{node: "local", nodes: []string{"local"}, writers: 4, entries: 50}
	if v := os.Getenv("SPACEIDINDEX_TEST_NODE"); v != "" {
		s.node = v
		s.nodes = strings.Split(os.Getenv("SPACEIDINDEX_TEST_NODES"), ",")
		if !slices.Contains(s.nodes, s.node) {
			t.Fatalf("SPACEIDINDEX_TEST_NODES (%q) has to contain SPACEIDINDEX_TEST_NODE (%q)", os.Getenv("SPACEIDINDEX_TEST_NODES"), s.node)
		}
	}
	s.writers = envInt(t, "SPACEIDINDEX_TEST_WRITERS", s.writers)
	s.entries = envInt(t, "SPACEIDINDEX_TEST_ENTRIES", s.entries)
	if v := os.Getenv("SPACEIDINDEX_TEST_PAUSE"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			t.Fatalf("SPACEIDINDEX_TEST_PAUSE: %v", err)
		}
		s.pause = d
	}
	if base := os.Getenv("SPACEIDINDEX_TEST_DIR"); base != "" {
		s.dir = filepath.Join(base, t.Name())
		if err := os.MkdirAll(s.dir, 0700); err != nil {
			t.Fatal(err)
		}
	} else {
		s.dir = t.TempDir()
	}
	return s
}

func envInt(t *testing.T, name string, def int) int {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return n
}

func key(node string, writer, i int) string { return fmt.Sprintf("%s-w%d-%d", node, writer, i) }

// addEntries is one writer: its own Index instance, entries adds one after the other.
func addEntries(dir, node string, writer, entries int, pause time.Duration) error {
	idx := spaceidindex.New(dir, "by-type")
	if err := idx.Init(); err != nil {
		return err
	}
	for i := 0; i < entries; i++ {
		if err := idx.Add("project", key(node, writer, i), "../../../spaces/"+key(node, writer, i)+"/nodes/x"); err != nil {
			return err
		}
		time.Sleep(pause)
	}
	return nil
}

// check waits until every node is done (marker files) and then expects the entries of all writers of all nodes
// in a freshly loaded index.
func check(t *testing.T, s setup) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(s.dir, "done-"+s.node), nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Minute)
	for _, n := range s.nodes {
		for {
			if _, err := os.Stat(filepath.Join(s.dir, "done-"+n)); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("node %s did not finish", n)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	links, err := spaceidindex.New(s.dir, "by-type").Load("project")
	if err != nil {
		t.Fatal(err)
	}
	missing := 0
	for _, n := range s.nodes {
		for w := 0; w < s.writers; w++ {
			for i := 0; i < s.entries; i++ {
				if _, ok := links[key(n, w, i)]; !ok {
					missing++
				}
			}
		}
	}
	want := len(s.nodes) * s.writers * s.entries
	t.Logf("RESULT %s node %s: %d of %d entries in the index (%d entries total)", t.Name(), s.node, want-missing, want, len(links))
	if missing > 0 {
		t.Errorf("%d of %d entries are missing from the index", missing, want)
	}
}

func TestConcurrentAddGoroutines(t *testing.T) {
	s := newSetup(t)
	var wg sync.WaitGroup
	errs := make(chan error, s.writers)
	for w := 0; w < s.writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			errs <- addEntries(s.dir, s.node, w, s.entries, s.pause)
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	check(t, s)
}

func TestConcurrentAddProcesses(t *testing.T) {
	s := newSetup(t)
	cmds := make([]*exec.Cmd, s.writers)
	outs := make([]*strings.Builder, s.writers)
	for w := range cmds {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperWriter$")
		cmd.Env = append(os.Environ(),
			helperEnv+"="+strings.Join([]string{s.dir, s.node, strconv.Itoa(w), strconv.Itoa(s.entries), s.pause.String()}, "|"))
		outs[w] = &strings.Builder{}
		cmd.Stdout, cmd.Stderr = outs[w], outs[w]
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds[w] = cmd
	}
	for w, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("writer %d: %v\n%s", w, err, outs[w])
		}
	}
	check(t, s)
}

// TestHelperWriter is one writer process of TestConcurrentAddProcesses; it does nothing when run directly.
func TestHelperWriter(t *testing.T) {
	v := os.Getenv(helperEnv)
	if v == "" {
		t.Skip("helper process of TestConcurrentAddProcesses")
	}
	p := strings.Split(v, "|")
	if len(p) != 5 {
		t.Fatalf("%s: %q", helperEnv, v)
	}
	w, err := strconv.Atoi(p[2])
	if err != nil {
		t.Fatal(err)
	}
	entries, err := strconv.Atoi(p[3])
	if err != nil {
		t.Fatal(err)
	}
	pause, err := time.ParseDuration(p[4])
	if err != nil {
		t.Fatal(err)
	}
	if err := addEntries(p[0], p[1], w, entries, pause); err != nil {
		t.Fatal(err)
	}
}

// TestIndexFileIsAlwaysComplete reads the index file without any lock while a writer adds entries. The file on disk
// has to hold a complete index at any time, so that a process that dies in the middle of an update does not leave
// an empty or partial index behind.
func TestIndexFileIsAlwaysComplete(t *testing.T) {
	dir := t.TempDir()
	idx := spaceidindex.New(dir, "by-type")
	if err := idx.Init(); err != nil {
		t.Fatal(err)
	}
	if err := idx.Add("project", "first", "../../../spaces/first/nodes/x"); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(dir, "by-type", "project.mpk")

	errs := make(chan error, 1)
	go func() {
		for i := 0; i < 200; i++ {
			if err := idx.Add("project", key("local", 0, i), "../../../spaces/x/nodes/x"); err != nil {
				errs <- err
				return
			}
		}
		errs <- nil
	}()

	reads, incomplete := 0, 0
	for writing := true; writing; {
		select {
		case err := <-errs:
			if err != nil {
				t.Fatal(err)
			}
			writing = false
		default:
		}
		b, err := os.ReadFile(indexPath)
		if err != nil {
			t.Fatal(err)
		}
		reads++
		links := map[string]string{}
		if len(b) == 0 || msgpack.Unmarshal(b, &links) != nil || links["first"] == "" {
			incomplete++
		}
	}
	if incomplete > 0 {
		t.Errorf("%d of %d reads found an empty or partial index file", incomplete, reads)
	}
}

// TestIndexSurvivesKilledWriter kills a process with SIGKILL while it updates an index (like an OOM kill or a pod
// killed after its grace period) and expects the index to be readable afterwards, with all entries written before.
// It needs no shared filesystem.
func TestIndexSurvivesKilledWriter(t *testing.T) {
	const prefilled = 5000
	runs := 20
	if testing.Short() {
		runs = 5
	}
	dir := t.TempDir()
	idx := spaceidindex.New(dir, "by-type")
	if err := idx.Init(); err != nil {
		t.Fatal(err)
	}
	m := make(map[string]string, prefilled)
	for i := 0; i < prefilled; i++ {
		m[fmt.Sprintf("pre-%d", i)] = fmt.Sprintf("../../../spaces/pre-%d/nodes/x", i)
	}
	if err := idx.AddAll("project", m); err != nil {
		t.Fatal(err)
	}

	for r := 0; r < runs; r++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperLoopWriter$")
		cmd.Env = append(os.Environ(), loopHelperEnv+"="+dir+"|"+strconv.Itoa(r))
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(200*time.Millisecond + rand.N(400*time.Millisecond))
		// a writer that ended by itself has failed to update the index; the check below tells why
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Fatal(err)
		}
		_ = cmd.Wait()

		links, err := spaceidindex.New(dir, "by-type").Load("project")
		if err != nil {
			t.Fatalf("kill %d: the index is not readable anymore: %v", r+1, err)
		}
		left := 0
		for k := range m {
			if _, ok := links[k]; ok {
				left++
			}
		}
		if left != prefilled {
			t.Fatalf("kill %d: %d of %d entries left in the index", r+1, left, prefilled)
		}
	}
}

// TestHelperLoopWriter is the writer process of TestIndexSurvivesKilledWriter: it adds entries until it is killed.
// It does nothing when run directly.
func TestHelperLoopWriter(t *testing.T) {
	v := os.Getenv(loopHelperEnv)
	if v == "" {
		t.Skip("helper process of TestIndexSurvivesKilledWriter")
	}
	p := strings.Split(v, "|")
	idx := spaceidindex.New(p[0], "by-type")
	for i := 0; ; i++ {
		k := fmt.Sprintf("loop-%s-%d", p[1], i)
		if err := idx.Add("project", k, "../../../spaces/"+k+"/nodes/x"); err != nil {
			t.Fatal(err)
		}
	}
}
