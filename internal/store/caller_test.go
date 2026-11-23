package store

import "testing"

// A pid read back from the store is a float64, as JSON reads every number. A
// Linux pid can be above 2^24 (pid_max runs to 4194304, and the ones past a
// million are common on a busy runner), so pidIn must give those back exactly,
// as it does the short pids of a mac.
func TestPidInReadsALongPidAsItWasWritten(t *testing.T) {
	t.Parallel()
	for _, pid := range []int{12345, 3865836, 4194304, 1 << 24, 1<<24 + 1} {
		for _, v := range []any{pid, float64(pid)} {
			got, ok := pidIn(v)
			if !ok || got != pid {
				t.Errorf("pidIn(%T %v) = %d, %v; want %d, true", v, v, got, ok, pid)
			}
		}
	}
	if _, ok := pidIn(float64(0)); ok {
		t.Error("pidIn(0) reports a pid; a zero pid is no pid")
	}
}
