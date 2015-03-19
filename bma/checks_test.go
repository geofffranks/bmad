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

