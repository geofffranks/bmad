package bma

import "testing"
import "os/exec"
import "github.com/stretchr/testify/assert"
import "time"
import "bytes"


func Test_merge_checks(t *testing.T) {
	old := Check{
		Command:       "echo \"third success\"",
		Every:         30,
		Retry_every:   25,
		Retries:       10,
		Timeout:       20,
		Env:           map[string]string{},
		Name:          "third",
		cmd_args:      []string{"echo", "third success"},
		next_run:      time.Unix(42,0),
		started_at:    time.Unix(15,0),
		ended_at:      time.Unix(20,0),
		duration:      5,
		attempts:      2,
		rc:            2,
		latency:       1345,
		stdout:        &bytes.Buffer{},
		stderr:        &bytes.Buffer{},
		sig_term:      true,
		sig_kill:      true,
		process:       &exec.Cmd{},
		running:       true,
	}
	c := Check{
		Command:     "echo \"new command\"",
		Every:       40,
		Retry_every: 27,
		Retries:     15,
		Timeout:     22,
		Env:         map[string]string{"env": "value"},
		Name:        "third",
		cmd_args:    []string{"echo","new command"},
	}
	expect := Check{
		Command:       "echo \"new command\"",
		Every:         40,
		Retry_every:   27,
		Retries:       15,
		Timeout:       22,
		Env:           map[string]string{"env": "value"},
		Name:          "third",
		cmd_args:      []string{"echo", "new command"},
		next_run:      time.Unix(42,0),
		started_at:    time.Unix(15,0),
		ended_at:      time.Unix(20,0),
		duration:      5,
		attempts:      2,
		rc:            2,
		latency:       1345,
		stdout:        old.stdout,
		stderr:        old.stderr,
		sig_term:      true,
		sig_kill:      true,
		process:       old.process,
		running:       true,
	}

	merge_checks(&c, &old)
	assert.Equal(t, expect, c, "merge_checks() merges all relevant data from old into new")
}

func Test_ShouldRun(t *testing.T) {
	check := Check{}

	assert.Equal(t, true, check.ShouldRun(), "check should run when running is false and next run is before now")

	check.running = true
	assert.Equal(t, false, check.ShouldRun(), "check shouldn't run if running already")

	check.running = false
	check.next_run = time.Now().Add(1 * time.Hour)
	assert.Equal(t, false, check.ShouldRun(), "check shouldn't run if next run is in the future")

	check.next_run = time.Now().Add(-1 * time.Hour)
	assert.Equal(t, true, check.ShouldRun(), "check should run when next_run exists and is in the past")
}

func Test_environment(t *testing.T) {
	check := Check{
		Env: map[string]string{
			"env1": "val1",
			"env2": "val2",
		},
	}
	expect := []string{"env1=val1", "env2=val2"}
	assert.Equal(t, expect, check.environment(), "Env gets translated into array of env vars correctly")
}

func Test_schedule(t *testing.T) {
	check := Check{
		Every:     300,
	}
	check.schedule(time.Unix(42,0), 0)
	assert.Equal(t, time.Unix(342, 0), check.next_run, "scheduling a check without an interval uses Every")

	check.schedule(time.Unix(42,0), 60)
	assert.Equal(t, time.Unix(102,0), check.next_run, "scheduling a check with an interval uses that interval")
}
