package deadcodefix

import "testing"

func TestRunnerAndDiff(t *testing.T) {
	if Runner(3) == 0 {
		t.Fail()
	}
	if testOnlyFn(5, 2) != 3 {
		t.Fail()
	}
}
