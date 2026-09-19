package interop_test

// The Python client answering the shared comparison corpus, and the rest of
// its wire-fidelity suite with it.
//
// testdata/compare.wire holds every case the comparison core has to get right,
// and each implementation of it -- Go, C, Python -- answers the same file with
// its own parser and its own comparison. Running the Python suite from here
// means a disagreement between the three fails `go test ./...` rather than
// waiting for someone to remember to run unittest.

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestThePythonClientAnswersTheComparisonCorpus(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skipf("python3 not available: %v", err)
	}
	cmd := exec.Command(python, "-m", "unittest", "discover", "-s",
		filepath.Join("tests"), "-v")
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err != nil {
		t.Fatalf("the Python suite failed: %v\n%s", err, text)
	}
	// Each corpus named, so a suite that silently stopped discovering one of
	// them is a failure here rather than a gap nobody notices.
	for _, ran := range []string{
		"test_every_case_answers_the_same_way",        // compare.wire
		"test_every_query_case_answers_the_same_way",  // query.wire
		"test_every_answer_case_answers_the_same_way", // answer.wire
		"test_every_stale_case_answers_the_same_way",  // stale.wire
	} {
		if !strings.Contains(text, ran) {
			t.Errorf("%s did not run:\n%s", ran, text)
		}
	}
}
